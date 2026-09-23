import { DataSourceInstanceSettings } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';

import {
  ChatRequest,
  ChatResponse,
  FlintAiJsonData,
  FlintAiQuery,
  GenerateChartRequest,
  GenerateChartResponse,
  ModelsResponse,
  RepairChartRequest,
  TestConnectionRequest,
  TestConnectionResponse,
} from './types';

export class DataSource extends DataSourceWithBackend<FlintAiQuery, FlintAiJsonData> {
  constructor(instanceSettings: DataSourceInstanceSettings<FlintAiJsonData>) {
    super(instanceSettings);
  }

  generate(input: GenerateChartRequest, signal?: AbortSignal): Promise<GenerateChartResponse> {
    return this.postResource<GenerateChartResponse>('/generate', input, { abortSignal: signal });
  }

  repair(input: RepairChartRequest, signal?: AbortSignal): Promise<GenerateChartResponse> {
    return this.postResource<GenerateChartResponse>('/repair', input, {
      abortSignal: signal,
      showSuccessAlert: false,
    });
  }

  chat(input: ChatRequest, signal?: AbortSignal): Promise<ChatResponse> {
    return this.postResource<ChatResponse>('/chat', input, {
      abortSignal: signal,
      showSuccessAlert: false,
    });
  }

  listModels(signal?: AbortSignal): Promise<ModelsResponse> {
    return this.getResource<ModelsResponse>('/models', undefined, { abortSignal: signal });
  }

  testConnection(input: TestConnectionRequest, signal?: AbortSignal): Promise<TestConnectionResponse> {
    return this.postResource<TestConnectionResponse>('/test', input, { abortSignal: signal });
  }

  health(): Promise<{ configured: boolean }> {
    return this.getResource<{ configured: boolean }>('/health');
  }
}
