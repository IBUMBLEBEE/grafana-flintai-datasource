import { DataSourceSettings } from '@grafana/data';

import { changeProvider, normalizeProviderKind, resetApiKey } from './configHelpers';
import { DEFAULT_FLINT_AI_PROVIDER_KIND, FlintAiJsonData, FlintAiSecureJsonData } from './types';

describe('Flint AI secure configuration', () => {
  it('resets only the API key and its configured marker', () => {
    const options = {
      jsonData: {
        baseUrl: 'https://api.example.test/v1',
        model: 'model-a',
        providerKind: DEFAULT_FLINT_AI_PROVIDER_KIND,
      },
      secureJsonData: {},
      secureJsonFields: { apiKey: true, unrelated: true },
    } as unknown as DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>;

    expect(resetApiKey(options)).toMatchObject({
      jsonData: options.jsonData,
      secureJsonData: { apiKey: '' },
      secureJsonFields: { apiKey: false, unrelated: true },
    });
  });

  it('migrates the previous provider kind to OpenAI', () => {
    expect(normalizeProviderKind('openai-compatible')).toBe('openai');
    expect(normalizeProviderKind('deepseek')).toBe('deepseek');
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
});
