import { AgentMessageConfig } from "@/types/datamodel";
import { fetchApi, createErrorResponse } from "./utils";

/**
 * Feedback issue types
 */
export enum FeedbackIssueType {
  INSTRUCTIONS = "instructions", // Did not follow instructions
  FACTUAL = "factual", // Not factually correct
  INCOMPLETE = "incomplete", // Incomplete response
  TOOL = "tool", // Should have run the tool
  OTHER = "other", // Other
}

/**
 * Feedback data structure that will be sent to the API
 */
export interface FeedbackData {
  // Whether the feedback is positive
  isPositive: boolean;

  // The feedback text provided by the user
  feedbackText: string;

  // The type of issue for negative feedback
  issueType?: FeedbackIssueType;

  // Content of the message that received feedback
  messageContent: string;

  // Source of the message (agent name)
  messageSource: string;

  // Contents of messages preceding the feedback
  precedingMessagesContents: string[];

  // Session information
  sessionInfo?: string;

  // Timestamp of the feedback submission
  timestamp: string;

  // Client information
  clientInfo?: Record<string, any>;
}

/**
 * Submit feedback to the server
 */
async function submitFeedback(feedbackData: FeedbackData): Promise<any> {
  try {
    const response = await fetchApi('/feedback', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(feedbackData),
    });
    
    return {
      success: true,
      data: response,
      message: 'Feedback submitted successfully'
    };
  } catch (error) {
    return createErrorResponse(error, 'Failed to submit feedback');
  }
}

/**
 * Submit positive feedback for an agent response
 */
export async function submitPositiveFeedback(
  message: AgentMessageConfig,
  precedingMessages: AgentMessageConfig[],
  feedbackText: string
) {
  // Create feedback data object
  const feedbackData: FeedbackData = {
    messageContent: typeof message.content === 'string' ? message.content : JSON.stringify(message.content),
    messageSource: message.source,
    isPositive: true,
    feedbackText,
    precedingMessagesContents: precedingMessages.map(m => {
      return typeof m.content === 'string' ? m.content : JSON.stringify(m.content);
    }),
    sessionInfo: message.metadata?.sessionId,
    timestamp: new Date().toISOString(),
  };
  
  console.log("Submitting positive feedback");
  
  // Submit feedback to the server
  return await submitFeedback(feedbackData);
}

/**
 * Submit negative feedback for an agent response
 */
export async function submitNegativeFeedback(
  message: AgentMessageConfig,
  precedingMessages: AgentMessageConfig[],
  feedbackText: string,
  issueType?: string
) {
  // Create feedback data object
  const feedbackData: FeedbackData = {
    messageContent: typeof message.content === 'string' ? message.content : JSON.stringify(message.content),
    messageSource: message.source,
    isPositive: false,
    feedbackText,
    issueType: issueType as FeedbackIssueType,
    precedingMessagesContents: precedingMessages.map(m => {
      return typeof m.content === 'string' ? m.content : JSON.stringify(m.content);
    }),
    sessionInfo: message.metadata?.sessionId,
    timestamp: new Date().toISOString(),
  };
  
  console.log("Submitting negative feedback");
  
  // Submit feedback to the server
  return await submitFeedback(feedbackData);
} 