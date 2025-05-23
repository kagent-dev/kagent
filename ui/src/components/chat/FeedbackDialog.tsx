import { useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { submitPositiveFeedback, submitNegativeFeedback } from "@/app/actions/feedback";
import { toast } from "sonner";

interface FeedbackDialogProps {
  isOpen: boolean;
  onClose: () => void;
  isPositive: boolean;
  messageId: number;
}

export function FeedbackDialog({ isOpen, onClose, isPositive, messageId }: FeedbackDialogProps) {
  const [feedbackText, setFeedbackText] = useState("");
  const [issueType, setIssueType] = useState<string | undefined>();
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleSubmit = async () => {
    setIsSubmitting(true);

    try {
      if (isPositive) {
        await submitPositiveFeedback(messageId, feedbackText);
      } else {
        await submitNegativeFeedback(messageId, feedbackText, issueType);
      }
      toast.success("Thank you for your feedback!");
      setFeedbackText("");
      setIssueType(undefined);
      onClose();
    } catch (error) {
      console.error("Error submitting feedback:", error);
      toast.error("Failed to submit feedback. Please try again.");
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleCancel = () => {
    setFeedbackText("");
    setIssueType(undefined);
    onClose();
  };

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>
            {isPositive ? "Provide Positive Feedback" : "Report an Issue"}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          {!isPositive && (
            <div className="grid grid-cols-4 items-center gap-4">
              <div className="text-sm font-medium col-span-4">
                <p className="mb-2">What type of issue did you encounter? (Optional)</p>
                <Select value={issueType} onValueChange={setIssueType}>
                  <SelectTrigger>
                    <SelectValue placeholder="Select issue type" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="instructions">Did not follow instructions</SelectItem>
                    <SelectItem value="factual">Not factually correct</SelectItem>
                    <SelectItem value="incomplete">Incomplete response</SelectItem>
                    <SelectItem value="tool">Should have run the tool</SelectItem>
                    <SelectItem value="other">Other</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
          )}
          <div className="grid grid-cols-4 items-center gap-4">
            <div className="text-sm font-medium col-span-4">
              <p className="mb-2">
                {isPositive 
                  ? "What was good about this response?" 
                  : "What was not good about this response?"}
              </p>
              <Textarea
                value={feedbackText}
                onChange={(e) => setFeedbackText(e.target.value)}
                placeholder="Please provide your feedback"
                className="col-span-3"
                rows={4}
              />
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!feedbackText || isSubmitting}>
            {isSubmitting ? "Submitting..." : "Submit"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
} 