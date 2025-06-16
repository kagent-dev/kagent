import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

interface HTMLPreviewDialogProps {
    html: string;
    open: boolean;
    onOpenChange: (open: boolean) => void;
}

const HTMLPreviewDialog = ({ html, open, onOpenChange }: HTMLPreviewDialogProps) => {
    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-4xl max-h-[80vh]">
                <DialogHeader>
                    <DialogTitle>HTML Preview</DialogTitle>
                </DialogHeader>
                <div className="mt-4">
                    <iframe
                        srcDoc={html}
                        className="w-full h-[60vh] border rounded-md"
                        title="HTML Preview"
                    />
                </div>
            </DialogContent>
        </Dialog>
    );
};


export default HTMLPreviewDialog;