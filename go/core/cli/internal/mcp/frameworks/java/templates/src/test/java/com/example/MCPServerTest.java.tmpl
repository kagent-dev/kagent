package com.example;

import com.example.tools.Tool;
import com.example.tools.Tools;
import com.fasterxml.jackson.databind.JsonNode;
import io.modelcontextprotocol.spec.McpSchema;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.util.HashMap;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Test class for MCP server functionality.
 */
public class MCPServerTest {
    
    private MCPServer server;
    
    @BeforeEach
    void setUp() {
        ServerConfig config = new ServerConfig();
        server = new MCPServer("test-server", config);
    }
    
    @AfterEach
    void tearDown() {
        if (server != null) {
            server.stop();
        }
    }
    
    @Test
    void testToolRegistration() {
        // Test that tools are properly registered
        Map<String, Tool> tools = Tools.getAllTools();
        assertFalse(tools.isEmpty(), "Should have at least one tool registered");
        
        // Test that echo tool is registered
        assertTrue(tools.containsKey("echo"), "Echo tool should be registered");
        
        Tool echoTool = tools.get("echo");
        assertEquals("echo", echoTool.getName());
        assertEquals("Echo a message back to the client", echoTool.getDescription());
    }
    
    @Test
    void testEchoToolExecution() throws Exception {
        // Test echo tool execution
        Tool echoTool = Tools.getTool("echo");
        assertNotNull(echoTool, "Echo tool should exist");
        
        Map<String, Object> parameters = new HashMap<>();
        parameters.put("message", "Hello, World!");
        
        Object result = echoTool.execute(parameters);
        assertNotNull(result, "Tool execution should return a result");
        
        // The result should contain the echoed message
        assertTrue(result.toString().contains("Hello, World!"), 
                  "Result should contain the echoed message");
    }
    
    @Test
    void testEchoToolInputSchema() {
        // Test that echo tool has proper input schema
        Tool echoTool = Tools.getTool("echo");
        McpSchema.JsonSchema schema = echoTool.getInputSchema();
        
        assertNotNull(schema, "Input schema should not be null");
        assertEquals("object", schema.type(), "Schema type should be object");
        
        Map<String, Object> properties = schema.properties();
        assertNotNull(properties, "Schema should have properties");
        assertTrue(properties.containsKey("message"), "Schema should have message property");
        
        @SuppressWarnings("unchecked")
        Map<String, Object> messageProperty = (Map<String, Object>) properties.get("message");
        assertEquals("string", messageProperty.get("type"), 
                    "Message property should be string type");
    }
    
    @Test
    void testEchoToolMissingParameter() {
        // Test echo tool with missing parameter
        Tool echoTool = Tools.getTool("echo");
        
        Map<String, Object> parameters = new HashMap<>();
        // Don't add message parameter
        
        assertThrows(IllegalArgumentException.class, () -> {
            echoTool.execute(parameters);
        }, "Should throw exception when message parameter is missing");
    }
    
    @Test
    void testServerConfiguration() {
        // Test server configuration
        ServerConfig testConfig = new ServerConfig();
        
        // Test default values
        assertEquals("stdio", testConfig.getTransport());
        assertEquals("localhost", testConfig.getHost());
        assertEquals(3000, testConfig.getPort());
        
        // Test setting values
        testConfig.setTransport("http");
        testConfig.setHost("127.0.0.1");
        testConfig.setPort(8080);
        
        assertEquals("http", testConfig.getTransport());
        assertEquals("127.0.0.1", testConfig.getHost());
        assertEquals(8080, testConfig.getPort());
    }
    
    @Test
    void testToolsRegistry() {
        // Test tools registry functionality
        assertTrue(Tools.hasTool("echo"), "Should have echo tool");
        assertTrue(Tools.getToolCount() >= 1, "Should have at least one tool registered");
        
        assertTrue(Tools.getToolNames().contains("echo"), 
                  "Tool names should include echo");
    }
}
