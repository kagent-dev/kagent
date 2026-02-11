"use client";

import { useState, useCallback, useRef, useEffect } from "react";

// Web Speech API types (not in all TypeScript DOM libs)
interface SpeechRecognitionResult {
  isFinal: boolean;
  length: number;
  item(index: number): SpeechRecognitionAlternative;
  [index: number]: SpeechRecognitionAlternative;
}
interface SpeechRecognitionAlternative {
  transcript: string;
  confidence: number;
}
interface SpeechRecognitionResultList {
  length: number;
  item(index: number): SpeechRecognitionResult;
  [index: number]: SpeechRecognitionResult;
}
interface SpeechRecognitionEvent extends Event {
  resultIndex: number;
  results: SpeechRecognitionResultList;
}
interface SpeechRecognitionErrorEvent extends Event {
  error: string;
  message?: string;
}
interface SpeechRecognitionInstance extends EventTarget {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  onstart: (() => void) | null;
  onend: (() => void) | null;
  onerror: ((event: SpeechRecognitionErrorEvent) => void) | null;
  onresult: ((event: SpeechRecognitionEvent) => void) | null;
  start(): void;
  stop(): void;
  abort(): void;
}
interface SpeechRecognitionConstructor {
  new (): SpeechRecognitionInstance;
}

declare global {
  interface Window {
    SpeechRecognition?: SpeechRecognitionConstructor;
    webkitSpeechRecognition?: SpeechRecognitionConstructor;
  }
}

export interface UseSpeechRecognitionOptions {
  onResult?: (transcript: string, isFinal: boolean) => void;
  onError?: (error: string) => void;
  language?: string;
  continuous?: boolean;
  interimResults?: boolean;
}

export interface UseSpeechRecognitionReturn {
  isListening: boolean;
  transcript: string;
  isSupported: boolean;
  startListening: () => void;
  stopListening: () => void;
  resetTranscript: () => void;
  error: string | null;
}

export function useSpeechRecognition(
  options: UseSpeechRecognitionOptions = {}
): UseSpeechRecognitionReturn {
  const {
    onResult,
    onError,
    language = "en-US",
    continuous = true,
    interimResults = true,
  } = options;

  const [isListening, setIsListening] = useState(false);
  const [transcript, setTranscript] = useState("");
  const [error, setError] = useState<string | null>(null);

  const recognitionRef = useRef<SpeechRecognitionInstance | null>(null);
  const isSupported =
    typeof window !== "undefined" &&
    (typeof window.SpeechRecognition !== "undefined" || typeof window.webkitSpeechRecognition !== "undefined");

  const resetTranscript = useCallback(() => {
    setTranscript("");
  }, []);

  const stopListening = useCallback(() => {
    if (!recognitionRef.current || !isListening) return;
    try {
      recognitionRef.current.stop();
    } catch (err) {
      // stop() can throw if recognition already ended; safe to ignore
      if (process.env.NODE_ENV === "development") {
        console.debug("[useSpeechRecognition] stop:", err);
      }
    }
    recognitionRef.current = null;
    setIsListening(false);
  }, [isListening]);

  const startListening = useCallback(() => {
    if (!isSupported) {
      setError("Speech recognition is not supported in this browser.");
      onError?.("Speech recognition is not supported in this browser.");
      return;
    }

    setError(null);
    setTranscript("");

    const SpeechRecognitionAPI = window.SpeechRecognition || window.webkitSpeechRecognition;
    if (!SpeechRecognitionAPI) return;
    const recognition = new SpeechRecognitionAPI() as SpeechRecognitionInstance;

    recognition.continuous = continuous;
    recognition.interimResults = interimResults;
    recognition.lang = language;

    recognition.onstart = () => {
      setIsListening(true);
    };

    recognition.onend = () => {
      setIsListening(false);
      recognitionRef.current = null;
    };

    recognition.onerror = (event: SpeechRecognitionErrorEvent) => {
      setIsListening(false);
      const message =
        event.error === "not-allowed"
          ? "Microphone access was denied."
          : event.error === "no-speech"
            ? "No speech detected. Try again."
            : `Speech recognition error: ${event.error}`;
      setError(message);
      onError?.(message);
    };

    recognition.onresult = (event: SpeechRecognitionEvent) => {
      let fullTranscript = "";
      let isFinal = false;
      for (let i = 0; i < event.results.length; i++) {
        const result = event.results[i];
        fullTranscript += result[0].transcript;
        if (result.isFinal) isFinal = true;
      }
      if (fullTranscript) {
        setTranscript(fullTranscript);
        onResult?.(fullTranscript, isFinal);
      }
    };

    recognitionRef.current = recognition;
    try {
      recognition.start();
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Failed to start speech recognition.";
      setError(message);
      onError?.(message);
      recognitionRef.current = null;
      setIsListening(false);
    }
  }, [isSupported, continuous, interimResults, language, onResult, onError]);

  useEffect(() => {
    return () => {
      const recognition = recognitionRef.current;
      if (!recognition) return;
      try {
        recognition.abort();
      } catch (err) {
        if (process.env.NODE_ENV === "development") {
          console.debug("[useSpeechRecognition] cleanup abort:", err);
        }
      }
      recognitionRef.current = null;
    };
  }, []);

  return {
    isListening,
    transcript,
    isSupported,
    startListening,
    stopListening,
    resetTranscript,
    error,
  };
}
