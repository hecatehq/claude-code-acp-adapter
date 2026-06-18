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
	info AdapterInfo
}

func NewServer(info AdapterInfo) *Server {
	return &Server{info: info}
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
	encoder := json.NewEncoder(output)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			if err := encoder.Encode(errorResponse(nil, -32700, "parse error", err.Error())); err != nil {
				return err
			}
			continue
		}
		if req.ID == nil {
			continue
		}
		if err := encoder.Encode(s.handle(req)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handle(req request) response {
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return errorResponse(req.ID, -32600, "invalid request", "jsonrpc must be 2.0")
	}

	switch req.Method {
	case "initialize":
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
		return errorResponse(req.ID, -32004, "not implemented", fmt.Sprintf("%s is not implemented in this scaffold", req.Method))
	default:
		return errorResponse(req.ID, -32601, "method not found", req.Method)
	}
}

type request struct {
	JSONRPC string           `json:"jsonrpc,omitempty"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"`
	Result  any              `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func resultResponse(id *json.RawMessage, result any) response {
	return response{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id *json.RawMessage, code int, message string, data any) response {
	return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message, Data: data}}
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
