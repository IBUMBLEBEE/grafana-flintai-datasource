import React, { ChangeEvent } from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { Field, FieldSet, Input, RadioButtonGroup, SecretInput } from '@grafana/ui';

import { changeProvider, normalizeProviderKind, PROVIDER_DEFAULTS, resetApiKey } from './configHelpers';
import { FlintAiJsonData, FlintAiProviderKind, FlintAiSecureJsonData } from './types';

type Props = DataSourcePluginOptionsEditorProps<FlintAiJsonData, FlintAiSecureJsonData>;

export const ConfigEditor = ({ options, onOptionsChange }: Props) => {
  const providerKind = normalizeProviderKind(options.jsonData.providerKind);
  const jsonData = {
    ...options.jsonData,
    baseUrl: options.jsonData.baseUrl ?? '',
    model: options.jsonData.model ?? '',
    providerKind,
  };

  const updateJsonData = (key: 'baseUrl' | 'model', value: string) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, [key]: value },
    });
  };

  const onProviderChange = (nextProviderKind: FlintAiProviderKind) => {
    onOptionsChange({
      ...options,
      jsonData: changeProvider(jsonData, nextProviderKind),
    });
  };

  const onApiKeyChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: { ...options.secureJsonData, apiKey: event.currentTarget.value },
    });
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
      <Field
        label="Base URL"
        description="API root without /chat/completions. HTTPS is required unless the backend explicitly enables local HTTP development."
        required
      >
        <Input
          width={60}
          value={jsonData.baseUrl}
          placeholder={PROVIDER_DEFAULTS[providerKind].baseUrl}
          onChange={(event) => updateJsonData('baseUrl', event.currentTarget.value)}
        />
      </Field>
      <Field label="Model" description="Default model for this provider instance." required>
        <Input
          width={60}
          value={jsonData.model}
          placeholder={PROVIDER_DEFAULTS[providerKind].modelPlaceholder}
          onChange={(event) => updateJsonData('model', event.currentTarget.value)}
        />
      </Field>
      <Field label="API key" description="Encrypted by Grafana and never returned to the browser." required>
        <SecretInput
          width={60}
          value={options.secureJsonData?.apiKey ?? ''}
          isConfigured={Boolean(options.secureJsonFields?.apiKey)}
          placeholder="Provider API key"
          onChange={onApiKeyChange}
          onReset={() => onOptionsChange(resetApiKey(options))}
        />
      </Field>
    </FieldSet>
  );
};
