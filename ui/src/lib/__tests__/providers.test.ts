import {
    isValidProviderInfoKey,
    getApiKeyForProviderFormKey,
    getProviderDisplayName,
    getProviderFormKey,
    PROVIDERS_INFO,
    modelProviders,
    BackendModelProviderType
} from '../providers';

describe('isValidProviderInfoKey', () => {
    it('should return true for valid provider keys', () => {
        expect(isValidProviderInfoKey('openai')).toBe(true);
        expect(isValidProviderInfoKey('azure-openai')).toBe(true);
        expect(isValidProviderInfoKey('anthropic')).toBe(true);
        expect(isValidProviderInfoKey('ollama')).toBe(true);
    });

    it('should return false for invalid provider keys', () => {
        expect(isValidProviderInfoKey('google')).toBe(false);
        expect(isValidProviderInfoKey('')).toBe(false);
        expect(isValidProviderInfoKey('OpenAI')).toBe(false); // Case sensitive
    });
});

describe('getApiKeyForProviderFormKey', () => {
    it('should return the correct API key string for each provider form key', () => {
        expect(getApiKeyForProviderFormKey('openai')).toBe('openAI');
        expect(getApiKeyForProviderFormKey('azure-openai')).toBe('azureOpenAI');
        expect(getApiKeyForProviderFormKey('anthropic')).toBe('anthropic');
        expect(getApiKeyForProviderFormKey('ollama')).toBe('ollama');
    });

    // Although the type system should prevent invalid keys, we test the default case
    it('should return the input key if it does not match known keys', () => {
        // @ts-expect-error - Testing invalid input
        expect(getApiKeyForProviderFormKey('unknown-provider')).toBe('unknown-provider');
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
        expect(getProviderFormKey('OpenAI')).toBe('openai');
        expect(getProviderFormKey('AzureOpenAI')).toBe('azure-openai');
        expect(getProviderFormKey('Anthropic')).toBe('anthropic');
        expect(getProviderFormKey('Ollama')).toBe('ollama');
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