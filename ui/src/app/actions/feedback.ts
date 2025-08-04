'use server'

import { FeedbackData, FeedbackIssueType } from "@/types";
import { fetchApi, getCurrentUserId } from "./utils";

/**
 * Submit feedback to the server
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function submitFeedback(feedbackData: FeedbackData): Promise<any> {
    const userID = await getCurrentUserId();
    const body = { 
        is_positive: feedbackData.isPositive,
        feedback_text: feedbackData.feedbackText,
        issue_type: feedbackData.issueType,
        message_id: feedbackData.messageId,
        user_id: userID
    };
    return await fetchApi('/feedback', {
        method: 'POST',
        body: JSON.stringify(body),
    });
}

/**
 * Submit positive feedback for an agent response
 */
export async function submitPositiveFeedback(
    message_id: number,
    feedback_text: string,
) {
    // Create feedback data object
    const feedbackData: FeedbackData = {
        isPositive: true,
        feedbackText: feedback_text,
        messageId: message_id,
    };
    return await submitFeedback(feedbackData);
}

/**
 * Submit negative feedback for an agent response
 */
export async function submitNegativeFeedback(
    message_id: number,
    feedback_text: string,
    issue_type?: string,
) {
    // Create feedback data object
    const feedbackData: FeedbackData = {
        isPositive: false,
        feedbackText: feedback_text,
        issueType: issue_type as FeedbackIssueType,
        messageId: message_id,
    };

    return await submitFeedback(feedbackData);
} 