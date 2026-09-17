import { DataSourceSettings } from '@grafana/data';

import { DEFAULT_FLINT_AI_PROVIDER_KIND, FlintAiJsonData, FlintAiProviderKind, FlintAiSecureJsonData } from './types';

export const PROVIDER_DEFAULTS: Record<FlintAiProviderKind, { baseUrl: string; modelPlaceholder: string }> = {
  openai: { baseUrl: 'https://api.openai.com/v1', modelPlaceholder: 'gpt-5' },
  deepseek: { baseUrl: 'https://api.deepseek.com/v1', modelPlaceholder: 'deepseek-chat' },
};

export function normalizeProviderKind(value: unknown): FlintAiProviderKind {
  if (value === 'deepseek') {
    return 'deepseek';
  }
  // Keep previously saved "openai-compatible" instances working as OpenAI.
  return DEFAULT_FLINT_AI_PROVIDER_KIND;
}

export function changeProvider(jsonData: FlintAiJsonData, providerKind: FlintAiProviderKind): FlintAiJsonData {
  const currentKind = normalizeProviderKind(jsonData.providerKind);
  const baseUrl = jsonData.baseUrl?.trim();
  const shouldUseProviderDefault = !baseUrl || baseUrl === PROVIDER_DEFAULTS[currentKind].baseUrl;

  return {
    ...jsonData,
    providerKind,
    baseUrl: shouldUseProviderDefault ? PROVIDER_DEFAULTS[providerKind].baseUrl : jsonData.baseUrl,
  };
}

export function resetApiKey(
  options: DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>
): DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData> {
  return {
    ...options,
    secureJsonFields: { ...options.secureJsonFields, apiKey: false },
    secureJsonData: { ...options.secureJsonData, apiKey: '' },
  };
}
