import { DataSourceInstanceSettings } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';

import {
  ChatRequest,
  ChatResponse,
  FlintAiJsonData,
  FlintAiQuery,
  GenerateChartRequest,
  GenerateChartResponse,
} from './types';

export class DataSource extends DataSourceWithBackend<FlintAiQuery, FlintAiJsonData> {
  constructor(instanceSettings: DataSourceInstanceSettings<FlintAiJsonData>) {
    super(instanceSettings);
  }

  generate(input: GenerateChartRequest, signal?: AbortSignal): Promise<GenerateChartResponse> {
    return this.postResource<GenerateChartResponse>('/generate', input, { abortSignal: signal });
  }

  chat(input: ChatRequest, signal?: AbortSignal): Promise<ChatResponse> {
    return this.postResource<ChatResponse>('/chat', input, { abortSignal: signal });
  }

  health(): Promise<{ configured: boolean }> {
    return this.getResource<{ configured: boolean }>('/health');
  }
}
