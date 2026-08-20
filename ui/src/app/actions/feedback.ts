'use server'

import { FeedbackData, FeedbackIssueType } from "@/types";
import { getFeedbackGrpcGateway } from "@/lib/grpc/client";

/**
 * Submit feedback to the server
 */
async function submitFeedback(feedbackData: FeedbackData) {
    const gateway = await getFeedbackGrpcGateway();
    await gateway.submitFeedback(feedbackData);
    return {
        error: false,
        data: {},
        message: "Feedback submitted successfully",
    };
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
