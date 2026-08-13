/**
 * Helper function to create a standardized error response
 * @param error The error object
 * @param defaultMessage Default error message if the error doesn't have a message
 * @returns A BaseResponse object with error information
 */
export function createErrorResponse<T>(error: unknown, defaultMessage: string): { message: string; error: string; data?: T } {
  const errorMessage = error instanceof Error ? error.message : defaultMessage;
  console.error(defaultMessage, error);
  return { message: errorMessage, error: errorMessage };
}
