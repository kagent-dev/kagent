import {
    isValidProviderInfoKey,
    getProviderDisplayName,
    getProviderFormKey,
    PROVIDERS_INFO,
    modelProviders,
    BackendModelProviderType
} from '../providers';

describe('isValidProviderInfoKey', () => {
    it('should return true for valid provider keys', () => {
        expect(isValidProviderInfoKey('OpenAI')).toBe(true);
        expect(isValidProviderInfoKey('AzureOpenAI')).toBe(true);
        expect(isValidProviderInfoKey('Anthropic')).toBe(true);
        expect(isValidProviderInfoKey('Ollama')).toBe(true);
        expect(isValidProviderInfoKey('Gemini')).toBe(true);
        expect(isValidProviderInfoKey('GeminiVertexAI')).toBe(true);
        expect(isValidProviderInfoKey('AnthropicVertexAI')).toBe(true);
    });

    it('should return false for invalid provider keys', () => {
        expect(isValidProviderInfoKey('google')).toBe(false);
        expect(isValidProviderInfoKey('')).toBe(false);
        expect(isValidProviderInfoKey('openai')).toBe(false); // Case sensitive
    });
});

describe('getProviderDisplayName', () => {
    it('should return the correct display name for each backend provider type', () => {
        expect(getProviderDisplayName('OpenAI')).toBe('OpenAI');
        expect(getProviderDisplayName('AzureOpenAI')).toBe('Azure OpenAI');
        expect(getProviderDisplayName('Anthropic')).toBe('Anthropic');
        expect(getProviderDisplayName('Ollama')).toBe('Ollama');
    });

    it('should return the input type if no matching provider is found', () => {
        expect(getProviderDisplayName('UnknownProvider' as BackendModelProviderType)).toBe('UnknownProvider');
    });
});

describe('getProviderFormKey', () => {
    it('should return the correct form key for each backend provider type', () => {
        expect(getProviderFormKey('OpenAI')).toBe('OpenAI');
        expect(getProviderFormKey('AzureOpenAI')).toBe('AzureOpenAI');
        expect(getProviderFormKey('Anthropic')).toBe('Anthropic');
        expect(getProviderFormKey('Ollama')).toBe('Ollama');
        expect(getProviderFormKey('Gemini')).toBe('Gemini');
        expect(getProviderFormKey('GeminiVertexAI')).toBe('GeminiVertexAI');
        expect(getProviderFormKey('AnthropicVertexAI')).toBe('AnthropicVertexAI');
    });

    it('should return undefined if no matching provider type is found', () => {
        expect(getProviderFormKey('UnknownProvider' as BackendModelProviderType)).toBeUndefined();
    });
});

// Optional: Add a test to ensure PROVIDERS_INFO keys match modelProviders array
describe('Provider Data Consistency', () => {
    it('should have PROVIDERS_INFO keys match modelProviders array elements', () => {
        const providerInfoKeys = Object.keys(PROVIDERS_INFO);
        expect(providerInfoKeys.sort()).toEqual([...modelProviders].sort());
    });

    it('should have a unique type for each provider', () => {
        const types = Object.values(PROVIDERS_INFO).map(info => info.type);
        const uniqueTypes = new Set(types);
        expect(types.length).toBe(uniqueTypes.size);
    });
}); 