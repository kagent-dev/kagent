import React, { useState } from "react";
import { MemoryQueryEvent, Message } from "@/types/datamodel";
import { ChevronDown } from "lucide-react";

interface MemoryQueryDisplayProps {
  currentMessage: Message;
}

const MemoryQueryDisplay = ({ currentMessage }: MemoryQueryDisplayProps) => {
  const [isExpanded, setIsExpanded] = useState(false);
  const eventData = currentMessage.config as MemoryQueryEvent;
  const toggleExpand = () => setIsExpanded(!isExpanded);

  return (
    <div className="border-l-2 border-l-violet-500 py-2 px-4">
      <div onClick={toggleExpand} className="cursor-pointer flex items-center hover:underline">
        <div className="text-xs font-bold">Memory retrieval</div>
        <ChevronDown className={`ml-2 h-4 w-4 transition-transform duration-300 ${isExpanded ? 'rotate-180' : ''}`} />
        
      </div>

      {isExpanded && (
        <div className="mt-2 pl-1 text-neutral-700">
          {eventData.content.length > 0 ? (
            <ul className="list-disc list-inside">
              {eventData.content.map((item, index: number) => (
                <li key={index} className="text-sm py-0.5"> 
                  {item.content || "No content"}
                  {item.mime_type && <span className="ml-2 text-xs text-neutral-500">({item.mime_type})</span>}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-neutral-500">No data retrieved.</p>
          )}
        </div>
      )}
    </div>
  );
};

export default MemoryQueryDisplay;
