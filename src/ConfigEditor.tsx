import React, { ChangeEvent, useRef, useState } from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { getBackendSrv, isFetchError } from '@grafana/runtime';
import {
  Alert,
  Button,
  Combobox,
  type ComboboxOption,
  Field,
  FieldSet,
  Input,
  RadioButtonGroup,
  SecretInput,
} from '@grafana/ui';

import {
  MODELS_LOAD_MESSAGES,
  PROVIDER_DEFAULTS,
  SavedConnectionSnapshot,
  applyProviderChange,
  buildModelComboboxOptions,
  canLoadSavedModels,
  classifyModelsLoadError,
  hasUnsavedConnectionEdits,
  isApiKeyConfigured,
  modelIdValidationError,
  modelsPreviewResourceUrl,
  modelsResourceUrl,
  parseProviderKind,
  pendingApiKeyValue,
  resetApiKey,
  testConnectionResourceUrl,
  withApiKeyEdit,
  withModelEdit,
  type ModelsLoadErrorKind,
} from './configHelpers';
import {
  DEFAULT_FLINT_AI_PROVIDER_KIND,
  FlintAiJsonData,
  FlintAiProviderKind,
  FlintAiSecureJsonData,
  ModelsPreviewRequest,
  ModelsResponse,
  TestConnectionRequest,
  TestConnectionResponse,
} from './types';

type Props = DataSourcePluginOptionsEditorProps<FlintAiJsonData, FlintAiSecureJsonData>;

async function fetchSavedModels(uid: string): Promise<string[]> {
  const response = await getBackendSrv().get<ModelsResponse>(modelsResourceUrl(uid));
  return Array.isArray(response?.models) ? response.models : [];
}

async function fetchPreviewModels(uid: string, input: ModelsPreviewRequest): Promise<string[]> {
  const response = await getBackendSrv().post<ModelsResponse>(modelsPreviewResourceUrl(uid), input);
  return Array.isArray(response?.models) ? response.models : [];
}

async function testAIConnection(uid: string, input: TestConnectionRequest): Promise<TestConnectionResponse> {
  return getBackendSrv().post<TestConnectionResponse>(testConnectionResourceUrl(uid), input);
}

function connectionTestErrorMessage(error: unknown): string {
  if (isFetchError(error)) {
    const detail = typeof error.data === 'string' ? error.data.trim() : error.statusText;
    return detail || 'AI connection test failed';
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return 'AI connection test failed';
}

function snapshotFromOptions(
  providerKind: FlintAiProviderKind,
  baseUrl: string,
  version?: number
): SavedConnectionSnapshot {
  return { providerKind, baseUrl, version };
}

export const ConfigEditor = ({ options, onOptionsChange }: Props) => {
  const configuredProviderKind = parseProviderKind(options.jsonData.providerKind);
  const providerKind = configuredProviderKind ?? DEFAULT_FLINT_AI_PROVIDER_KIND;
  const providerInvalid = Boolean(options.jsonData.providerKind) && !configuredProviderKind;
  const isNewProviderConfig = !options.jsonData.providerKind;
  const jsonData = {
    ...options.jsonData,
    baseUrl: options.jsonData.baseUrl || (isNewProviderConfig ? PROVIDER_DEFAULTS[providerKind].baseUrl : ''),
    model: options.jsonData.model ?? '',
    providerKind,
  };
  const editorOptions = { ...options, jsonData };

  const [savedConnection, setSavedConnection] = useState<SavedConnectionSnapshot>(() =>
    snapshotFromOptions(providerKind, jsonData.baseUrl, options.version)
  );
  const [seenVersion, setSeenVersion] = useState(options.version);

  // After Grafana saves, options.version bumps — refresh the baseline from the newly saved form values.
  if (options.version !== seenVersion) {
    setSeenVersion(options.version);
    setSavedConnection(snapshotFromOptions(providerKind, jsonData.baseUrl, options.version));
  }

  const [catalog, setCatalog] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [errorKind, setErrorKind] = useState<ModelsLoadErrorKind | null>(null);
  const [modelTouched, setModelTouched] = useState(false);
  const [testingConnection, setTestingConnection] = useState(false);
  const [connectionTestResult, setConnectionTestResult] = useState<{
    severity: 'success' | 'error';
    message: string;
  } | null>(null);
  const [customBaseUrlProvider, setCustomBaseUrlProvider] = useState<FlintAiProviderKind | null>(null);
  const catalogRequest = useRef(0);
  const connectionTestRequest = useRef(0);

  const connectionDirty = hasUnsavedConnectionEdits(jsonData, savedConnection);
  const apiKeyConfigured = isApiKeyConfigured(editorOptions, providerKind);
  const apiKeyValue = pendingApiKeyValue(options.secureJsonData, providerKind);
  const canLoadSaved = !providerInvalid && canLoadSavedModels(editorOptions, providerKind) && !connectionDirty;
  const canLoadPreview =
    !providerInvalid && Boolean(options.uid && jsonData.baseUrl.trim() && apiKeyValue.trim());
  const canLoad = canLoadSaved || canLoadPreview;
  const modelError = modelIdValidationError(jsonData.model);
  const showModelError = Boolean(modelError) && (modelTouched || jsonData.model !== '');

  const catalogOptions = buildModelComboboxOptions(catalog, jsonData.model);

  const invalidateCatalog = () => {
    catalogRequest.current += 1;
    setCatalog([]);
    setErrorKind(null);
    setLoading(false);
  };

  const invalidateConnectionTest = () => {
    connectionTestRequest.current += 1;
    setTestingConnection(false);
    setConnectionTestResult(null);
  };

  const updateJsonData = (key: 'baseUrl' | 'model', value: string) => {
    invalidateConnectionTest();
    onOptionsChange({
      ...options,
      jsonData: key === 'model' ? withModelEdit(jsonData, providerKind, value) : { ...jsonData, [key]: value },
    });
  };

  const onProviderChange = (nextProviderKind: FlintAiProviderKind) => {
    const baseUrl = jsonData.baseUrl.trim();
    const keepsCustomBaseUrl =
      providerKind !== nextProviderKind && baseUrl !== '' && baseUrl !== PROVIDER_DEFAULTS[providerKind].baseUrl;
    invalidateCatalog();
    setModelTouched(false);
    invalidateConnectionTest();
    setCustomBaseUrlProvider(keepsCustomBaseUrl ? nextProviderKind : null);
    onOptionsChange(applyProviderChange(editorOptions, nextProviderKind));
  };

  const onApiKeyChange = (event: ChangeEvent<HTMLInputElement>) => {
    invalidateCatalog();
    invalidateConnectionTest();
    onOptionsChange(withApiKeyEdit(editorOptions, providerKind, event.currentTarget.value));
  };

  const onModelPick = (option: ComboboxOption<string> | null) => {
    setModelTouched(true);
    updateJsonData('model', option?.value ?? '');
  };

  const loadModels = async () => {
    if (!canLoad || !options.uid) {
      setErrorKind('unsaved');
      return;
    }

    const requestID = ++catalogRequest.current;
    setLoading(true);
    setErrorKind(null);
    try {
      const models = canLoadPreview
        ? await fetchPreviewModels(options.uid, {
            providerKind,
            baseUrl: jsonData.baseUrl,
            apiKey: apiKeyValue,
          })
        : await fetchSavedModels(options.uid);
      if (requestID !== catalogRequest.current) {
        return;
      }
      setCatalog(models);
      setErrorKind(models.length === 0 ? 'empty' : null);
    } catch (error) {
      if (requestID !== catalogRequest.current) {
        return;
      }
      setCatalog([]);
      if (isFetchError(error)) {
        const message = typeof error.data === 'string' ? error.data : error.statusText;
        setErrorKind(classifyModelsLoadError(error.status, message));
      } else if (error instanceof Error) {
        setErrorKind(classifyModelsLoadError(undefined, error.message));
      } else {
        setErrorKind('unavailable');
      }
    } finally {
      if (requestID === catalogRequest.current) {
        setLoading(false);
      }
    }
  };

  const runConnectionTest = async () => {
    setModelTouched(true);
    setConnectionTestResult(null);

    if (!options.uid) {
      setConnectionTestResult({ severity: 'error', message: 'Save the datasource once before testing AI.' });
      return;
    }
    if (providerInvalid) {
      setConnectionTestResult({ severity: 'error', message: 'Select OpenAI or DeepSeek before testing AI.' });
      return;
    }
    if (!jsonData.baseUrl.trim()) {
      setConnectionTestResult({ severity: 'error', message: 'Base URL is required.' });
      return;
    }
    if (modelError) {
      setConnectionTestResult({ severity: 'error', message: modelError });
      return;
    }
    const canUseSavedKey = apiKeyConfigured && !connectionDirty;
    if (!apiKeyValue.trim() && !canUseSavedKey) {
      setConnectionTestResult({
        severity: 'error',
        message: 'Enter the API key to test unsaved Provider or Base URL changes.',
      });
      return;
    }

    setTestingConnection(true);
    const requestID = ++connectionTestRequest.current;
    try {
      const response = await testAIConnection(options.uid, {
        providerKind,
        baseUrl: jsonData.baseUrl.trim(),
        model: jsonData.model.trim(),
        ...(apiKeyValue.trim() ? { apiKey: apiKeyValue.trim() } : {}),
      });
      if (!response?.ok) {
        throw new Error(response?.message || 'AI connection test failed');
      }
      if (requestID === connectionTestRequest.current) {
        setConnectionTestResult({ severity: 'success', message: response.message || 'AI connection succeeded' });
      }
    } catch (error) {
      if (requestID === connectionTestRequest.current) {
        setConnectionTestResult({ severity: 'error', message: connectionTestErrorMessage(error) });
      }
    } finally {
      if (requestID === connectionTestRequest.current) {
        setTestingConnection(false);
      }
    }
  };

  return (
    <FieldSet label="AI provider">
      <Field
        label="Provider"
        description="Select the provider so Flint can use its supported request and structured-output format."
        required
      >
        <RadioButtonGroup<FlintAiProviderKind>
          value={providerKind}
          options={[
            { label: 'OpenAI', value: 'openai' },
            { label: 'DeepSeek', value: 'deepseek' },
          ]}
          onChange={onProviderChange}
        />
      </Field>
      {providerInvalid && (
        <Alert title="Unsupported provider configuration" severity="error">
          Select OpenAI or DeepSeek and save the datasource before using the provider.
        </Alert>
      )}
      <Field
        label="Base URL"
        description="API root without /chat/completions. HTTPS is required unless the backend explicitly enables local HTTP development."
        required
      >
        <Input
          width={60}
          value={jsonData.baseUrl}
          placeholder={PROVIDER_DEFAULTS[providerKind].baseUrl}
          onChange={(event) => {
            setCustomBaseUrlProvider(null);
            invalidateCatalog();
            updateJsonData('baseUrl', event.currentTarget.value);
          }}
        />
      </Field>
      {customBaseUrlProvider && (
        <Alert title="Custom Base URL retained" severity="warning">
          Confirm that this custom Base URL supports {customBaseUrlProvider === 'deepseek' ? 'DeepSeek' : 'OpenAI'}.
        </Alert>
      )}
      <Field
        label="API key"
        description="Encrypted by Grafana and never returned to the browser. OpenAI and DeepSeek each keep their own saved key — switching Provider does not clear the other."
        required
      >
        <SecretInput
          width={60}
          value={apiKeyValue}
          isConfigured={apiKeyConfigured}
          placeholder={`${providerKind === 'deepseek' ? 'DeepSeek' : 'OpenAI'} API key`}
          onChange={onApiKeyChange}
          onReset={() => {
            invalidateCatalog();
            invalidateConnectionTest();
            onOptionsChange(resetApiKey(editorOptions, providerKind));
          }}
        />
      </Field>
      <Field
        label={<span id="flint-ai-model-label">Model</span>}
        description="Open to load and search the provider catalog, or enter a custom model ID and press Enter. Catalog entries do not guarantee every Flint request shape."
        htmlFor="flint-ai-model"
        required
        invalid={showModelError || undefined}
        error={showModelError ? modelError : undefined}
      >
        <Combobox
          id="flint-ai-model"
          width={60}
          options={catalogOptions}
          value={jsonData.model || null}
          placeholder={PROVIDER_DEFAULTS[providerKind].modelPlaceholder}
          createCustomValue
          customValueDescription="Use as custom model ID"
          isClearable
          loading={loading}
          noOptionsMessage="No catalog models. Enter a model ID and press Enter."
          onIsOpenChange={(isOpen) => {
            if (isOpen && catalog.length === 0 && !loading && errorKind === null) {
              void loadModels();
            }
          }}
          onChange={onModelPick}
          aria-labelledby="flint-ai-model-label"
        />
      </Field>
      {errorKind && (
        <Alert title="Model list" severity={errorKind === 'empty' ? 'info' : 'warning'}>
          {MODELS_LOAD_MESSAGES[errorKind]}
        </Alert>
      )}
      <Field
        label="Test AI connection"
        description="Sends one minimal chat-completion request using the current settings. The provider may charge a small usage fee."
      >
        <Button type="button" variant="secondary" onClick={() => void runConnectionTest()} disabled={testingConnection}>
          {testingConnection ? 'Testing AI...' : 'Test AI connection'}
        </Button>
      </Field>
      {connectionTestResult && (
        <Alert
          title={connectionTestResult.severity === 'success' ? connectionTestResult.message : 'AI connection failed'}
          severity={connectionTestResult.severity}
        >
          {connectionTestResult.severity === 'error' ? connectionTestResult.message : undefined}
        </Alert>
      )}
    </FieldSet>
  );
};
