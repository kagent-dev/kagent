'use server'

import { AgentMessageConfig } from "@/types/datamodel";
import { FeedbackData, FeedbackIssueType } from "@/lib/types";
import { fetchApi, getCurrentUserId } from "./utils";

/**
 * Submit feedback to the server
 */
async function submitFeedback(feedbackData: FeedbackData): Promise<any> {
    const userID = await getCurrentUserId();
    const body = { ...feedbackData, userID };
    return await fetchApi('/feedback', {
        method: 'POST',
        body: JSON.stringify(body),
    });
}

/**
 * Submit positive feedback for an agent response
 */
export async function submitPositiveFeedback(
    message: AgentMessageConfig,
    precedingMessages: AgentMessageConfig[],
    feedbackText: string,
    sessionID?: string
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
        timestamp: new Date().toISOString(),
        sessionID: sessionID,
    };
    return await submitFeedback(feedbackData);
}

/**
 * Submit negative feedback for an agent response
 */
export async function submitNegativeFeedback(
    message: AgentMessageConfig,
    precedingMessages: AgentMessageConfig[],
    feedbackText: string,
    issueType?: string,
    sessionID?: string
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
        timestamp: new Date().toISOString(),
        sessionID: sessionID,
    };

    return await submitFeedback(feedbackData);
} 