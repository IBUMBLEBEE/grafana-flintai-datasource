import { DataSourceSettings } from '@grafana/data';

import { FlintAiJsonData, FlintAiProviderKind, FlintAiSecureJsonData } from './types';

export const PROVIDER_DEFAULTS: Record<FlintAiProviderKind, { baseUrl: string; modelPlaceholder: string }> = {
  openai: { baseUrl: 'https://api.openai.com/v1', modelPlaceholder: 'Select or enter a model ID' },
  deepseek: { baseUrl: 'https://api.deepseek.com/v1', modelPlaceholder: 'Select or enter a model ID' },
};

export const MAX_MODEL_ID_BYTES = 256;

/** Datasource resource path for model discovery. Never append secrets or provider hosts. */
export const MODELS_RESOURCE_PATH = 'models';
export const MODELS_PREVIEW_RESOURCE_PATH = 'models/preview';
export const TEST_CONNECTION_RESOURCE_PATH = 'test';

export type ModelsLoadErrorKind = 'unsaved' | 'auth' | 'unavailable' | 'network' | 'empty';

export const MODELS_LOAD_MESSAGES: Record<ModelsLoadErrorKind, string> = {
  unsaved:
    'Enter a Base URL and API key to load models; you can also enter a model ID and press Enter',
  auth: 'Authentication failed; check the API key',
  unavailable: 'Model catalog is unavailable for this Base URL. Enter a model ID and press Enter.',
  network: 'Could not reach the provider. Enter a model ID and press Enter, or try again after editing the connection.',
  empty: 'No models returned. Enter a model ID and press Enter.',
};

export type ApiKeySecureField = 'openaiApiKey' | 'deepseekApiKey';
export type ProviderModelField = 'openaiModel' | 'deepseekModel';

export function parseProviderKind(value: unknown): FlintAiProviderKind | undefined {
  return value === 'openai' || value === 'deepseek' ? value : undefined;
}

export function apiKeySecureField(providerKind: FlintAiProviderKind): ApiKeySecureField {
  return providerKind === 'deepseek' ? 'deepseekApiKey' : 'openaiApiKey';
}

export function providerModelField(providerKind: FlintAiProviderKind): ProviderModelField {
  return providerKind === 'deepseek' ? 'deepseekModel' : 'openaiModel';
}

export function changeProvider(jsonData: FlintAiJsonData, providerKind: FlintAiProviderKind): FlintAiJsonData {
  const currentKind = jsonData.providerKind;
  const baseUrl = jsonData.baseUrl?.trim();
  const shouldUseProviderDefault = !baseUrl || baseUrl === PROVIDER_DEFAULTS[currentKind].baseUrl;

  return {
    ...jsonData,
    providerKind,
    baseUrl: shouldUseProviderDefault ? PROVIDER_DEFAULTS[providerKind].baseUrl : jsonData.baseUrl,
  };
}

/** True when this provider has a saved key. */
export function isApiKeyConfigured(
  options: {
    secureJsonFields?: Record<string, boolean | undefined>;
  },
  providerKind: FlintAiProviderKind
): boolean {
  const field = apiKeySecureField(providerKind);
  return Boolean(options.secureJsonFields?.[field]);
}

export function pendingApiKeyValue(
  secureJsonData: FlintAiSecureJsonData | undefined,
  providerKind: FlintAiProviderKind
): string {
  const field = apiKeySecureField(providerKind);
  return secureJsonData?.[field] ?? '';
}

export function resetApiKey(
  options: DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>,
  providerKind: FlintAiProviderKind = options.jsonData.providerKind
): DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData> {
  const field = apiKeySecureField(providerKind);

  return {
    ...options,
    secureJsonFields: {
      ...options.secureJsonFields,
      [field]: false,
    },
    secureJsonData: {
      ...options.secureJsonData,
      [field]: '',
    },
  };
}

/**
 * Switch Provider. Each provider keeps its own API key and model selection; `model`
 * remains the active provider's backend-facing value.
 */
export function applyProviderChange(
  options: DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>,
  nextProviderKind: FlintAiProviderKind
): DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData> {
  const currentKind = parseProviderKind(options.jsonData.providerKind);
  const currentModel = options.jsonData.model ?? '';
  const rememberedJsonData = {
    ...options.jsonData,
    ...(currentKind ? { [providerModelField(currentKind)]: currentModel } : {}),
  };
  const jsonData = changeProvider(
    {
      ...rememberedJsonData,
      baseUrl: options.jsonData.baseUrl ?? '',
      model: currentModel,
      providerKind: currentKind ?? nextProviderKind,
    },
    nextProviderKind
  );

  if (currentKind === nextProviderKind) {
    return { ...options, jsonData };
  }

  return {
    ...options,
    jsonData: { ...jsonData, model: rememberedJsonData[providerModelField(nextProviderKind)] ?? '' },
  };
}

export function withModelEdit(
  jsonData: FlintAiJsonData,
  providerKind: FlintAiProviderKind,
  model: string
): FlintAiJsonData {
  const normalizedModel = model.trim();
  return {
    ...jsonData,
    model: normalizedModel,
    [providerModelField(providerKind)]: normalizedModel,
  };
}

export function modelIdValidationError(model: string): string | undefined {
  const normalizedModel = model.trim();
  if (!normalizedModel) {
    return 'Model is required';
  }

  let bytes = 0;
  for (const character of normalizedModel) {
    const codePoint = character.codePointAt(0) ?? 0;
    bytes += codePoint <= 0x7f ? 1 : codePoint <= 0x7ff ? 2 : codePoint <= 0xffff ? 3 : 4;
  }
  return bytes > MAX_MODEL_ID_BYTES ? `Model ID must be ${MAX_MODEL_ID_BYTES} bytes or fewer` : undefined;
}

export function withApiKeyEdit(
  options: DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>,
  providerKind: FlintAiProviderKind,
  apiKey: string
): DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData> {
  const field = apiKeySecureField(providerKind);
  return {
    ...options,
    secureJsonData: {
      ...options.secureJsonData,
      [field]: apiKey,
    },
  };
}

/** Map a backend /models failure to UI copy. Auth must not be confused with gateway path failures. */
export function classifyModelsLoadError(
  status?: number,
  message?: string
): Exclude<ModelsLoadErrorKind, 'unsaved' | 'empty'> {
  if (status === 401 || status === 403) {
    return 'auth';
  }
  const text = (message ?? '').toLowerCase();
  if (
    status === undefined ||
    text.includes('could not reach') ||
    text.includes('network') ||
    text.includes('timeout')
  ) {
    return 'network';
  }
  return 'unavailable';
}

export type SavedConnectionSnapshot = {
  providerKind: FlintAiProviderKind;
  baseUrl: string;
  version?: number;
};

/** True when the instance is saved with an API key for the current provider and the user is not mid-edit of that key. */
export function canLoadSavedModels(
  options: {
    uid?: string;
    secureJsonFields?: Record<string, boolean | undefined>;
    secureJsonData?: FlintAiSecureJsonData;
    jsonData?: { providerKind?: string };
  },
  providerKind: FlintAiProviderKind | undefined = parseProviderKind(options.jsonData?.providerKind)
): boolean {
  if (!providerKind) {
    return false;
  }
  const pending = pendingApiKeyValue(options.secureJsonData, providerKind);
  return Boolean(options.uid) && isApiKeyConfigured(options, providerKind) && !pending;
}

export function hasUnsavedConnectionEdits(
  current: { providerKind?: string; baseUrl?: string },
  saved: SavedConnectionSnapshot
): boolean {
  return current.providerKind !== saved.providerKind || (current.baseUrl ?? '').trim() !== saved.baseUrl.trim();
}

export function modelsResourceUrl(uid: string): string {
  return `/api/datasources/uid/${encodeURIComponent(uid)}/resources/${MODELS_RESOURCE_PATH}`;
}

export function modelsPreviewResourceUrl(uid: string): string {
  return `/api/datasources/uid/${encodeURIComponent(uid)}/resources/${MODELS_PREVIEW_RESOURCE_PATH}`;
}

export function testConnectionResourceUrl(uid: string): string {
  return `/api/datasources/uid/${encodeURIComponent(uid)}/resources/${TEST_CONNECTION_RESOURCE_PATH}`;
}

export function buildModelComboboxOptions(
  modelIds: string[],
  selectedModel: string
): Array<{ label: string; value: string }> {
  const ids = new Set<string>();
  for (const id of modelIds) {
    const trimmed = id.trim();
    if (trimmed) {
      ids.add(trimmed);
    }
  }
  const selected = selectedModel.trim();
  if (selected) {
    ids.add(selected);
  }
  return [...ids].sort().map((id) => ({ label: id, value: id }));
}
