package acp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxMessageBytes = 1024 * 1024

type AdapterInfo struct {
	Name         string
	Title        string
	Version      string
	Capabilities Capabilities
}

type Capabilities struct {
	Images          bool
	EmbeddedContext bool
	MCPHTTP         bool
	MCPSSE          bool
}

type Server struct {
	info              AdapterInfo
	initialize        any
	initializeHandler InitializeHandler
	methods           map[string]MethodHandler
	notifications     map[string]NotificationHandler
}

type Option func(*Server)

type InitializeHandler func(params json.RawMessage) (any, *RPCError)

type MethodHandler func(ctx *MethodContext, params json.RawMessage) (any, *RPCError)

type NotificationHandler func(params json.RawMessage) error

type MethodContext struct {
	encoder *json.Encoder
	conn    *connection
}

func (c *MethodContext) Notify(method string, params any) error {
	return c.encoder.Encode(serverNotification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (c *MethodContext) Request(method string, params any) (json.RawMessage, *RPCError, error) {
	if c == nil || c.conn == nil {
		return nil, nil, errors.New("method context is not connected")
	}
	id := c.conn.nextRequestID()
	if err := c.encoder.Encode(serverRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		return nil, nil, err
	}
	for {
		msg, ok, err := c.conn.readMessage()
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, io.EOF
		}
		if msg.ID != nil && msg.Method == "" && string(*msg.ID) == string(id) {
			return append(json.RawMessage(nil), msg.Result...), msg.Error, nil
		}
		if msg.ID == nil {
			continue
		}
		return nil, nil, fmt.Errorf("unexpected message while waiting for response to %s", string(id))
	}
}

func WithMethod(method string, handler MethodHandler) Option {
	return func(s *Server) {
		s.methods[method] = handler
	}
}

func WithNotification(method string, handler NotificationHandler) Option {
	return func(s *Server) {
		s.notifications[method] = handler
	}
}

func WithInitializeResult(result any) Option {
	return func(s *Server) {
		s.initialize = result
	}
}

func WithInitializeHandler(handler InitializeHandler) Option {
	return func(s *Server) {
		s.initializeHandler = handler
	}
}

func NewServer(info AdapterInfo, opts ...Option) *Server {
	server := &Server{
		info:          info,
		methods:       map[string]MethodHandler{},
		notifications: map[string]NotificationHandler{},
	}
	for _, opt := range opts {
		opt(server)
	}
	return server
}

func (s *Server) Serve(input io.Reader, output io.Writer) error {
	if input == nil {
		return errors.New("input is required")
	}
	if output == nil {
		return errors.New("output is required")
	}

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	conn := &connection{
		scanner: scanner,
		encoder: json.NewEncoder(output),
	}

	for {
		req, ok, err := conn.readMessage()
		if err != nil {
			var parseErr parseMessageError
			if !errors.As(err, &parseErr) {
				return err
			}
			if err := conn.encoder.Encode(errorResponse(nil, -32700, "parse error", parseErr.Err.Error())); err != nil {
				return err
			}
			continue
		}
		if !ok {
			break
		}
		if req.Method == "" {
			continue
		}
		if req.ID == nil {
			if handler := s.notifications[req.Method]; handler != nil {
				if err := handler(req.Params); err != nil {
					return err
				}
			}
			continue
		}
		ctx := &MethodContext{encoder: conn.encoder, conn: conn}
		if err := conn.encoder.Encode(s.handle(ctx, request{
			JSONRPC: req.JSONRPC,
			ID:      req.ID,
			Method:  req.Method,
			Params:  req.Params,
		})); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handle(ctx *MethodContext, req request) response {
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return errorResponse(req.ID, -32600, "invalid request", "jsonrpc must be 2.0")
	}

	switch req.Method {
	case "initialize":
		if s.initializeHandler != nil {
			result, rpcErr := s.initializeHandler(req.Params)
			if rpcErr != nil {
				return response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
			}
			return resultResponse(req.ID, result)
		}
		if s.initialize != nil {
			return resultResponse(req.ID, s.initialize)
		}
		return resultResponse(req.ID, initializeResult{
			ProtocolVersion: 1,
			AgentCapabilities: agentCapabilities{
				PromptCapabilities: promptCapabilities{
					Image:           s.info.Capabilities.Images,
					EmbeddedContext: s.info.Capabilities.EmbeddedContext,
				},
				MCPCapabilities: mcpCapabilities{
					HTTP: s.info.Capabilities.MCPHTTP,
					SSE:  s.info.Capabilities.MCPSSE,
				},
			},
			AgentInfo: agentInfo{
				Name:    s.info.Name,
				Title:   s.info.Title,
				Version: s.info.Version,
			},
			AuthMethods: []authMethod{},
		})
	case "session/new", "session/load", "session/resume", "session/list", "session/prompt", "session/cancel", "session/close":
		if handler := s.methods[req.Method]; handler != nil {
			result, rpcErr := handler(ctx, req.Params)
			if rpcErr != nil {
				return response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
			}
			return resultResponse(req.ID, result)
		}
		return errorResponse(req.ID, -32004, "not implemented", fmt.Sprintf("%s is not implemented in this scaffold", req.Method))
	default:
		if handler := s.methods[req.Method]; handler != nil {
			result, rpcErr := handler(ctx, req.Params)
			if rpcErr != nil {
				return response{JSONRPC: "2.0", ID: req.ID, Error: rpcErr}
			}
			return resultResponse(req.ID, result)
		}
		return errorResponse(req.ID, -32601, "method not found", req.Method)
	}
}

type request struct {
	JSONRPC string           `json:"jsonrpc,omitempty"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type message struct {
	JSONRPC string           `json:"jsonrpc,omitempty"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

type serverNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type serverRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  any             `json:"params,omitempty"`
}

type response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  any              `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func resultResponse(id *json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id *json.RawMessage, code int, message string, data any) response {
	return response{JSONRPC: "2.0", ID: id, Error: &RPCError{Code: code, Message: message, Data: data}}
}

type initializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities agentCapabilities `json:"agentCapabilities"`
	AgentInfo         agentInfo         `json:"agentInfo"`
	AuthMethods       []authMethod      `json:"authMethods"`
}

type agentCapabilities struct {
	PromptCapabilities promptCapabilities `json:"promptCapabilities"`
	MCPCapabilities    mcpCapabilities    `json:"mcpCapabilities"`
}

type promptCapabilities struct {
	Image           bool `json:"image"`
	EmbeddedContext bool `json:"embeddedContext"`
}

type mcpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse,omitempty"`
}

type agentInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type authMethod struct{}

type connection struct {
	scanner *bufio.Scanner
	encoder *json.Encoder
	nextID  int64
}

func (c *connection) readMessage() (message, bool, error) {
	for c.scanner.Scan() {
		line := c.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg message
		if err := json.Unmarshal(line, &msg); err != nil {
			return message{}, false, parseMessageError{Err: err}
		}
		return msg, true, nil
	}
	if err := c.scanner.Err(); err != nil {
		return message{}, false, err
	}
	return message{}, false, nil
}

func (c *connection) nextRequestID() json.RawMessage {
	c.nextID++
	raw, err := json.Marshal(fmt.Sprintf("server-%d", c.nextID))
	if err != nil {
		panic(err)
	}
	return raw
}

type parseMessageError struct {
	Err error
}

func (e parseMessageError) Error() string {
	return e.Err.Error()
}

func (e parseMessageError) Unwrap() error {
	return e.Err
}
