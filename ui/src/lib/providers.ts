export type BackendModelProviderType = "OpenAI" | "AzureOpenAI" | "Anthropic" | "Ollama" | "Gemini"; // ADDED: "Gemini"
export const modelProviders = ["openai", "azure-openai", "anthropic", "ollama", "gemini"] as const; // ADDED: "gemini"
export type ModelProviderKey = typeof modelProviders[number];


export const PROVIDERS_INFO: {
    [key in ModelProviderKey]: {
        name: string; // Display name (e.g., "OpenAI")
        type: BackendModelProviderType; // Backend type (e.g., "OpenAI")
        apiKeyLink: string | null; // Link to get API key
        modelDocsLink?: string; // Link to model documentation (Optional)
        help: string; // Help text
    }
} = {
    openai: {
        name: "OpenAI",
        type: "OpenAI",
        apiKeyLink: "https://platform.openai.com/settings/api-keys",
        modelDocsLink: "https://github.com/kagent-dev/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/openai/_model_info.py",
        help: "Get your API key from the OpenAI API Keys page."
    },
    "azure-openai": {
        name: "Azure OpenAI",
        type: "AzureOpenAI",
        apiKeyLink: "https://portal.azure.com/",
        modelDocsLink: "https://github.com/kagent-dev/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/openai/_model_info.py",
        help: "Find your Endpoint and Key in your Azure OpenAI resource."
    },
    anthropic: {
        name: "Anthropic",
        type: "Anthropic",
        apiKeyLink: "https://console.anthropic.com/settings/keys",
        modelDocsLink: "https://github.com/kagent-dev/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/anthropic/_model_info.py",
        help: "Get your API key from the Anthropic Console."
    },
    ollama: {
        name: "Ollama",
        type: "Ollama",
        apiKeyLink: null,
        modelDocsLink: "https://github.com/kagent-dev/autogen/blob/main/python/packages/autogen-ext/src/autogen_ext/models/ollama/_model_info.py",
        help: "No API key needed. Ensure Ollama is running and accessible."
    },
    // ADDED: Gemini Provider Info
    gemini: {
        name: "Google Gemini", // A user-friendly display name
        type: "Gemini", // Matches your backend Go enum (v1alpha1.ModelProvider)
        apiKeyLink: "https://aistudio.google.com/app/apikey", // Link to Google AI Studio API key page
        modelDocsLink: "https://ai.google.dev/models/gemini", // Link to Gemini model documentation
        help: "Get your API key from Google AI Studio. Note: Google Cloud API Keys for Generative AI may also work."
    },
};

export const isValidProviderInfoKey = (key: string): key is ModelProviderKey => {
    return key in PROVIDERS_INFO;
};

// Helper to map form key (lowercase, hyphenated) to API key (camelCase or specific strings)
export const getApiKeyForProviderFormKey = (providerFormKey: ModelProviderKey): string => {
    switch (providerFormKey) {
        case 'openai': return 'openAI';
        case 'azure-openai': return 'azureOpenAI';
        case 'anthropic': return 'anthropic';
        case 'ollama': return 'ollama';
        case 'gemini': return 'gemini'; // ADDED: Gemini API key string, matches backend constant if 'GEMINI_API_KEY'
        default: return providerFormKey; // Fallback
    }
};

// Helper to get the display name from the backend type
export const getProviderDisplayName = (providerType: BackendModelProviderType): string => {
    for (const key in PROVIDERS_INFO) {
        if (PROVIDERS_INFO[key as ModelProviderKey].type === providerType) {
            return PROVIDERS_INFO[key as ModelProviderKey].name;
        }
    }
    return providerType;
}

// Helper to get the provider form key from the backend type
export const getProviderFormKey = (providerType: BackendModelProviderType): ModelProviderKey | undefined => {
     for (const key in PROVIDERS_INFO) {
        if (PROVIDERS_INFO[key as ModelProviderKey].type === providerType) {
            return key as ModelProviderKey;
        }
    }
    return undefined;
}
