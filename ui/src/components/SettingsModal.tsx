'use client'
import { useEffect, useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { useUserStore } from "@/lib/userStore";

interface SettingsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function SettingsModal({ open, onOpenChange }: SettingsModalProps) {
  const { userId, setUserId } = useUserStore();
  const [inputValue, setInputValue] = useState(userId);

  useEffect(() => {
    setInputValue(userId);
  }, [userId]);

  const handleSave = () => {
    if (inputValue.trim()) {
      setUserId(inputValue.trim());
      onOpenChange(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[425px] bg-[#2A2A2A] text-white border-[#3A3A3A]">
        <DialogHeader>
          <DialogTitle className="text-white">Settings</DialogTitle>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          <div className="grid grid-cols-4 items-center gap-4">
            <Label htmlFor="userId" className="text-right text-white/70">
              User ID
            </Label>
            <Input id="userId" value={inputValue} onChange={(e) => setInputValue(e.target.value)} className="col-span-3 bg-[#1A1A1A] border-[#3A3A3A] text-white" placeholder="Enter user ID" />
          </div>
        </div>
        <DialogFooter>
          <Button type="submit" onClick={handleSave} className="bg-white hover:bg-white/90 text-black">
            Save changes
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
