import { DataSourceSettings } from '@grafana/data';

import {
  applyProviderChange,
  buildModelComboboxOptions,
  canLoadSavedModels,
  changeProvider,
  classifyModelsLoadError,
  hasUnsavedConnectionEdits,
  isApiKeyConfigured,
  MAX_MODEL_ID_BYTES,
  MODELS_LOAD_MESSAGES,
  MODELS_PREVIEW_RESOURCE_PATH,
  MODELS_RESOURCE_PATH,
  modelsPreviewResourceUrl,
  modelsResourceUrl,
  modelIdValidationError,
  parseProviderKind,
  resetApiKey,
  TEST_CONNECTION_RESOURCE_PATH,
  testConnectionResourceUrl,
  withApiKeyEdit,
} from './configHelpers';
import { DEFAULT_FLINT_AI_PROVIDER_KIND, FlintAiJsonData, FlintAiSecureJsonData } from './types';

describe('Flint AI secure configuration', () => {
  it('resets only the current provider API key and its configured marker', () => {
    const options = {
      jsonData: {
        baseUrl: 'https://api.deepseek.com/v1',
        model: 'model-a',
        providerKind: 'deepseek',
      },
      secureJsonData: { deepseekApiKey: '', openaiApiKey: '' },
      secureJsonFields: { deepseekApiKey: true, openaiApiKey: true, unrelated: true },
    } as unknown as DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>;

    expect(resetApiKey(options, 'deepseek')).toMatchObject({
      secureJsonData: { deepseekApiKey: '', openaiApiKey: '' },
      secureJsonFields: { deepseekApiKey: false, openaiApiKey: true, unrelated: true },
    });
  });

  it('accepts only exact supported provider values', () => {
    expect(parseProviderKind('openai')).toBe('openai');
    expect(parseProviderKind('deepseek')).toBe('deepseek');
    expect(parseProviderKind('openai-compatible')).toBeUndefined();
    expect(parseProviderKind('')).toBeUndefined();
    expect(parseProviderKind('gemini')).toBeUndefined();
  });

  it('updates a known provider URL without replacing a custom gateway', () => {
    const openAIConfig = {
      baseUrl: 'https://api.openai.com/v1',
      model: '',
      providerKind: 'openai',
    } as FlintAiJsonData;

    expect(changeProvider(openAIConfig, 'deepseek')).toMatchObject({
      providerKind: 'deepseek',
      baseUrl: 'https://api.deepseek.com/v1',
    });
    expect(
      changeProvider({ ...openAIConfig, baseUrl: 'https://ai-gateway.example.test/v1' }, 'deepseek')
    ).toMatchObject({
      providerKind: 'deepseek',
      baseUrl: 'https://ai-gateway.example.test/v1',
    });
  });

  it('initializes a new datasource when the first selected provider is DeepSeek', () => {
    const options = {
      jsonData: {},
      secureJsonData: {},
      secureJsonFields: {},
    } as unknown as DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>;

    expect(applyProviderChange(options, 'deepseek')).toMatchObject({
      jsonData: {
        providerKind: 'deepseek',
        baseUrl: 'https://api.deepseek.com/v1',
        model: '',
      },
    });
  });

  it('keeps each provider API key when switching and restores configured state on switch-back', () => {
    const options = {
      jsonData: {
        baseUrl: 'https://api.deepseek.com/v1',
        model: 'deepseek-flash',
        providerKind: 'deepseek',
      },
      secureJsonData: {},
      secureJsonFields: { deepseekApiKey: true, openaiApiKey: true },
    } as unknown as DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>;

    const toOpenAI = applyProviderChange(options, 'openai');
    expect(toOpenAI).toMatchObject({
      jsonData: {
        providerKind: 'openai',
        baseUrl: 'https://api.openai.com/v1',
        model: '',
      },
      secureJsonFields: { deepseekApiKey: true, openaiApiKey: true },
    });
    expect(isApiKeyConfigured(toOpenAI, 'openai')).toBe(true);
    expect(isApiKeyConfigured(toOpenAI, 'deepseek')).toBe(true);

    const backToDeepSeek = applyProviderChange(toOpenAI, 'deepseek');
    expect(backToDeepSeek.secureJsonFields).toMatchObject({ deepseekApiKey: true, openaiApiKey: true });
    expect(isApiKeyConfigured(backToDeepSeek, 'deepseek')).toBe(true);
  });

  it('restores each provider model after switching away and back', () => {
    const deepSeekOptions = {
      jsonData: {
        baseUrl: 'https://api.deepseek.com/v1',
        model: 'deepseek-chat',
        providerKind: 'deepseek',
      },
      secureJsonData: {},
      secureJsonFields: { deepseekApiKey: true, openaiApiKey: true },
    } as unknown as DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>;

    const openAIOptions = applyProviderChange(deepSeekOptions, 'openai');
    const withOpenAIModel = {
      ...openAIOptions,
      jsonData: { ...openAIOptions.jsonData, model: 'gpt-5' },
    };
    const backToDeepSeek = applyProviderChange(withOpenAIModel, 'deepseek');

    expect(openAIOptions.jsonData.deepseekModel).toBe('deepseek-chat');
    expect(backToDeepSeek.jsonData.model).toBe('deepseek-chat');
    expect(backToDeepSeek.jsonData.openaiModel).toBe('gpt-5');
  });

  it('writes API key edits into the active provider secure field', () => {
    const options = {
      jsonData: {
        baseUrl: 'https://api.openai.com/v1',
        model: '',
        providerKind: 'openai',
      },
      secureJsonData: {},
      secureJsonFields: {},
    } as unknown as DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>;

    expect(withApiKeyEdit(options, 'openai', 'sk-openai')).toMatchObject({
      secureJsonData: { openaiApiKey: 'sk-openai' },
    });
    expect(withApiKeyEdit(options, 'deepseek', 'sk-deepseek')).toMatchObject({
      secureJsonData: { deepseekApiKey: 'sk-deepseek' },
    });
  });

  it('keeps models discovery on the datasource resource path without secrets', () => {
    expect(MODELS_RESOURCE_PATH).toBe('models');
    expect(MODELS_PREVIEW_RESOURCE_PATH).toBe('models/preview');
    expect(TEST_CONNECTION_RESOURCE_PATH).toBe('test');
    expect(MODELS_RESOURCE_PATH.includes('apiKey')).toBe(false);
    expect(modelsResourceUrl('ds-1')).toBe('/api/datasources/uid/ds-1/resources/models');
    expect(modelsPreviewResourceUrl('ds-1')).toBe('/api/datasources/uid/ds-1/resources/models/preview');
    expect(testConnectionResourceUrl('ds-1')).toBe('/api/datasources/uid/ds-1/resources/test');
    expect(MODELS_LOAD_MESSAGES.auth).toMatch(/API key/i);
    expect(MODELS_LOAD_MESSAGES.unavailable).toMatch(/enter a model ID/i);
    expect(classifyModelsLoadError(401)).toBe('auth');
    expect(classifyModelsLoadError(404, 'Model list is unavailable for this Base URL; enter a model ID manually')).toBe(
      'unavailable'
    );
    expect(classifyModelsLoadError(undefined, 'Could not reach the provider; try again or enter a model ID')).toBe(
      'network'
    );
  });

  it('gates model loading on the current provider saved credentials', () => {
    expect(
      canLoadSavedModels({
        uid: 'flint',
        jsonData: { providerKind: 'openai' },
        secureJsonFields: { openaiApiKey: true },
        secureJsonData: {},
      })
    ).toBe(true);
    expect(
      canLoadSavedModels({
        uid: 'flint',
        jsonData: { providerKind: 'openai' },
        secureJsonFields: { deepseekApiKey: true },
        secureJsonData: {},
      })
    ).toBe(false);
    expect(
      canLoadSavedModels({
        uid: 'flint',
        jsonData: { providerKind: 'openai' },
        secureJsonFields: { openaiApiKey: true },
        secureJsonData: { openaiApiKey: 'pending' },
      })
    ).toBe(false);
    expect(
      canLoadSavedModels({
        uid: 'flint',
        jsonData: { providerKind: 'deepseek' },
        secureJsonFields: { apiKey: true },
        secureJsonData: {},
      })
    ).toBe(false);

    expect(
      hasUnsavedConnectionEdits(
        { providerKind: 'openai', baseUrl: 'https://api.openai.com/v1' },
        { providerKind: 'openai', baseUrl: 'https://api.openai.com/v1' }
      )
    ).toBe(false);
    expect(
      hasUnsavedConnectionEdits(
        { providerKind: 'deepseek', baseUrl: 'https://api.openai.com/v1' },
        { providerKind: 'openai', baseUrl: 'https://api.openai.com/v1' }
      )
    ).toBe(true);
  });

  it('keeps the selected model in combobox options for manual fallback', () => {
    expect(buildModelComboboxOptions(['gpt-b', 'gpt-a'], 'custom-id')).toEqual([
      { label: 'custom-id', value: 'custom-id' },
      { label: 'gpt-a', value: 'gpt-a' },
      { label: 'gpt-b', value: 'gpt-b' },
    ]);
  });

  it('validates custom model IDs using the backend byte limit', () => {
    expect(modelIdValidationError('  ')).toBe('Model is required');
    expect(modelIdValidationError('m'.repeat(MAX_MODEL_ID_BYTES))).toBeUndefined();
    expect(modelIdValidationError('m'.repeat(MAX_MODEL_ID_BYTES + 1))).toMatch(/256 bytes or fewer/);
    expect(modelIdValidationError('模'.repeat(85))).toBeUndefined();
    expect(modelIdValidationError('模'.repeat(86))).toMatch(/256 bytes or fewer/);
  });

  it('still exposes DEFAULT provider constant', () => {
    expect(DEFAULT_FLINT_AI_PROVIDER_KIND).toBe('openai');
  });
});
