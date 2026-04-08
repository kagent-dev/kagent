package com.example;

/**
 * Server configuration for MCP server.
 * 
 * This class handles configuration loading from environment variables.
 */
public class ServerConfig {
    private String transport = "stdio";
    private String host = "localhost";
    private int port = 3000;
    
    public ServerConfig() {
        loadEnvironmentConfig();
    }
    
    /**
     * Load configuration from environment variables.
     */
    private void loadEnvironmentConfig() {
        String envTransport = System.getenv("MCP_TRANSPORT_MODE");
        if (envTransport != null && !envTransport.isEmpty()) {
            this.transport = envTransport;
        }
        
        String envHost = System.getenv("HOST");
        if (envHost != null && !envHost.isEmpty()) {
            this.host = envHost;
        }
        
        String envPort = System.getenv("PORT");
        if (envPort != null && !envPort.isEmpty()) {
            try {
                this.port = Integer.parseInt(envPort);
            } catch (NumberFormatException e) {
                // Keep default port if invalid
            }
        }
    }
    
    /**
     * Get transport mode.
     */
    public String getTransport() {
        return transport;
    }
    
    /**
     * Set transport mode.
     */
    public void setTransport(String transport) {
        this.transport = transport;
    }
    
    /**
     * Get host.
     */
    public String getHost() {
        return host;
    }
    
    /**
     * Set host.
     */
    public void setHost(String host) {
        this.host = host;
    }
    
    /**
     * Get port.
     */
    public int getPort() {
        return port;
    }
    
    /**
     * Set port.
     */
    public void setPort(int port) {
        this.port = port;
    }
    
    @Override
    public String toString() {
        return String.format("ServerConfig{transport='%s', host='%s', port=%d}", 
                           transport, host, port);
    }
}
