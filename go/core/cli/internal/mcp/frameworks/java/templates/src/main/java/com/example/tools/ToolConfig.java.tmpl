package com.example.tools;

import java.util.Map;
import java.util.HashMap;

/**
 * Utility class for tool configuration.
 * 
 * This class provides methods to load tool-specific configuration
 * from environment variables.
 */
public class ToolConfig {
    
    /**
     * Get tool-specific configuration value.
     * 
     * @param toolName the name of the tool
     * @param key the configuration key
     * @param defaultValue the default value if not found
     * @return the configuration value
     */
    public static String getToolConfig(String toolName, String key, String defaultValue) {
        // Try environment variable first (format: TOOL_NAME_KEY)
        String envKey = toolName.toUpperCase() + "_" + key.toUpperCase();
        String envValue = System.getenv(envKey);
        if (envValue != null && !envValue.isEmpty()) {
            return envValue;
        }
        
        return defaultValue;
    }
    
    /**
     * Get tool-specific configuration value.
     * 
     * @param toolName the name of the tool
     * @param key the configuration key
     * @return the configuration value, or null if not found
     */
    public static String getToolConfig(String toolName, String key) {
        return getToolConfig(toolName, key, null);
    }
    
    /**
     * Get all configuration for a tool.
     * 
     * @param toolName the name of the tool
     * @return a map of all configuration values for the tool
     */
    public static Map<String, String> getAllToolConfig(String toolName) {
        Map<String, String> config = new HashMap<>();
        
        // Get all environment variables for this tool
        String prefix = toolName.toUpperCase() + "_";
        for (Map.Entry<String, String> entry : System.getenv().entrySet()) {
            if (entry.getKey().startsWith(prefix)) {
                String key = entry.getKey().substring(prefix.length()).toLowerCase();
                config.put(key, entry.getValue());
            }
        }
        
        return config;
    }
    
    /**
     * Check if a tool configuration exists.
     * 
     * @param toolName the name of the tool
     * @param key the configuration key
     * @return true if the configuration exists, false otherwise
     */
    public static boolean hasToolConfig(String toolName, String key) {
        return getToolConfig(toolName, key) != null;
    }
}
