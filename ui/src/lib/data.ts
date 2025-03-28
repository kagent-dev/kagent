import { AgentTool, Component, MCPToolConfig, ToolConfig } from "@/types/datamodel";

export const isMcpTool = (content: unknown): content is MCPToolConfig => {
  if (!content || typeof content !== "object") return false;

  const { config } = content as { config: unknown };
  if (!config || typeof config !== "object") return false;

  const { server_params, tool } = config as { server_params: unknown; tool: unknown };
  if (!server_params || typeof server_params !== "object" || !tool || typeof tool !== "object") return false;

  return true;
};

export const isAgentTool = (tool: unknown): tool is AgentTool => {
  if (!tool || typeof tool !== "object") return false;
  
  const possibleAgentTool = tool as Partial<AgentTool>;
  return typeof possibleAgentTool.provider === "string" && 
         typeof possibleAgentTool.description === "string" && 
         typeof possibleAgentTool.config === "object" && 
         possibleAgentTool.config !== null;
};

export const getToolDisplayName = (tool?: Component<ToolConfig> | AgentTool) => {
  if (!tool) return "No name";
  
  if (isMcpTool(tool)) {
    return (tool.config as MCPToolConfig).tool.name;
  } else if (isAgentTool(tool)) {
    return (tool as AgentTool).provider || "Agent Tool";
  } else {
    return tool?.label ?? "No name";
  }
};

export const getToolDescription = (tool?: Component<ToolConfig> | AgentTool) => {
  if (!tool) return "No description";
  
  if (isMcpTool(tool)) {
    return (tool.config as MCPToolConfig).tool.description;
  } else if (isAgentTool(tool)) {
    return (tool as AgentTool).description || "No description";
  } else {
    return tool?.description ?? "No description";
  }
};

export const getToolIdentifier = (tool?: Component<ToolConfig> | AgentTool): string => {
  if (!tool) return "unknown";
  
  if (isMcpTool(tool)) {
    return (tool.config as MCPToolConfig).tool.name;
  } else if (isAgentTool(tool)) {
    const agentTool = tool as AgentTool;
    return `${agentTool.provider}::${agentTool.provider || "Agent Tool"}`;
  } else {
    return `${tool?.provider}::${getToolDisplayName(tool)}`;
  }
};