import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

interface HTMLPreviewDialogProps {
    html: string;
    open: boolean;
    onOpenChange: (open: boolean) => void;
}

const HTMLPreviewDialog = ({ html, open, onOpenChange }: HTMLPreviewDialogProps) => {
    const sanitizeHTML = (html: string) => {
        // Create a temporary div to parse the HTML
        const temp = document.createElement('div');
        temp.innerHTML = html;

        // Remove script tags and their contents
        const scripts = temp.getElementsByTagName('script');
        while (scripts.length > 0) {
            scripts[0].parentNode?.removeChild(scripts[0]);
        }

        // Remove event handlers and other potentially dangerous attributes
        const elements = temp.getElementsByTagName('*');
        for (let i = 0; i < elements.length; i++) {
            const element = elements[i];
            const attributes = element.attributes;
            for (let j = attributes.length - 1; j >= 0; j--) {
                const attr = attributes[j];
                // Remove event handlers and other potentially dangerous attributes
                if (attr.name.startsWith('on') || 
                    attr.name === 'href' || 
                    attr.name === 'src' ||
                    attr.name === 'style') {
                    element.removeAttribute(attr.name);
                }
            }
        }

        return temp.innerHTML;
    };

    // Wrap the HTML in a sandboxed iframe with security restrictions
    const safeHTML = `
        <!DOCTYPE html>
        <html>
        <head>
            <meta charset="UTF-8">
            <meta name="viewport" content="width=device-width, initial-scale=1.0">
            <style>
                body { margin: 0; padding: 16px; }
                * { max-width: 100%; }
            </style>
        </head>
        <body>
            ${sanitizeHTML(html)}
        </body>
        </html>
    `;

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-w-4xl max-h-[80vh]">
                <DialogHeader>
                    <DialogTitle>HTML Preview</DialogTitle>
                </DialogHeader>
                <div className="mt-4">
                    <iframe
                        srcDoc={safeHTML}
                        className="w-full h-[60vh] border rounded-md"
                        title="HTML Preview"
                        sandbox="allow-same-origin"
                        referrerPolicy="no-referrer"
                    />
                </div>
            </DialogContent>
        </Dialog>
    );
};

export default HTMLPreviewDialog;