package forwarder

import (
	"testing"

	"cursor/gen/agentv1"
)

func TestMCPRegistrySnapshotRemovesDisabledServers(t *testing.T) {
	service := &Service{store: NewConversationFileStore(t.TempDir())}
	stream := &ActiveStream{ConversationID: "conversation"}

	withBoth := &agentv1.RequestContext{
		McpFileSystemOptions: &agentv1.McpFileSystemOptions{
			McpDescriptors: []*agentv1.McpDescriptor{
				{ServerIdentifier: "user-fast-context", ServerName: "fast-context", Tools: []*agentv1.McpToolDescriptor{{ToolName: "fast_context_search"}}},
				{ServerIdentifier: "server-b", ServerName: "mcp-b", Tools: []*agentv1.McpToolDescriptor{{ToolName: "tool_b"}}},
			},
		},
	}
	service.updateStreamMCPToolServers(stream, withBoth)
	if _, err := service.store.UpdateConversationMeta(stream.ConversationID, func(conversation *ConversationFile) error {
		conversation.MCPToolServers = snapshotStreamMCPToolServers(stream)
		return nil
	}); err != nil {
		t.Fatalf("persist initial MCP snapshot: %v", err)
	}

	service.updateStreamMCPToolServers(stream, &agentv1.RequestContext{
		McpFileSystemOptions: &agentv1.McpFileSystemOptions{
			McpDescriptors: []*agentv1.McpDescriptor{{ServerIdentifier: "server-b", ServerName: "mcp-b", Tools: []*agentv1.McpToolDescriptor{{ToolName: "tool_b"}}}},
		},
	})
	if _, err := service.store.UpdateConversationMeta(stream.ConversationID, func(conversation *ConversationFile) error {
		conversation.MCPToolServers = snapshotStreamMCPToolServers(stream)
		return nil
	}); err != nil {
		t.Fatalf("persist updated MCP snapshot: %v", err)
	}
	if server := lookupMCPToolServer(stream, "fast_context_search"); server != "" {
		t.Fatalf("disabled MCP server remained in active stream: %q", server)
	}

	conversation, err := service.store.LoadConversation(stream.ConversationID)
	if err != nil {
		t.Fatalf("load MCP snapshot: %v", err)
	}
	if _, found := conversation.MCPToolServers["fast_context_search"]; found {
		t.Fatal("disabled MCP tool remained in persisted snapshot")
	}
	if server := conversation.MCPToolServers["tool_b"]; server != "server-b" {
		t.Fatalf("enabled MCP server = %q, want server-b", server)
	}
}

func TestMCPRegistryResolvesServerNameAlias(t *testing.T) {
	stream := &ActiveStream{}
	service := &Service{}
	service.updateStreamMCPToolServers(stream, &agentv1.RequestContext{
		McpFileSystemOptions: &agentv1.McpFileSystemOptions{
			McpDescriptors: []*agentv1.McpDescriptor{{
				ServerIdentifier: "user-fast-context",
				ServerName:       "fast-context",
				Tools:            []*agentv1.McpToolDescriptor{{ToolName: "fast_context_search"}},
			}},
		},
	})

	if server := lookupMCPToolServer(stream, "fast-context"); server != "user-fast-context" {
		t.Fatalf("server alias = %q, want user-fast-context", server)
	}
	if server := lookupMCPToolServer(stream, "fast-context-fast_context_search"); server != "user-fast-context" {
		t.Fatalf("canonical alias = %q, want user-fast-context", server)
	}
}
