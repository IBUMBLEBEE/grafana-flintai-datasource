import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export const FLINT_AI_DATASOURCE_PLUGIN_ID = 'ibumblebee-flintai-datasource';
export const FLINT_AI_PROVIDER_KINDS = ['openai', 'deepseek'] as const;
export type FlintAiProviderKind = (typeof FLINT_AI_PROVIDER_KINDS)[number];
export const DEFAULT_FLINT_AI_PROVIDER_KIND: FlintAiProviderKind = 'openai';

export interface FlintAiJsonData extends DataSourceJsonData {
  baseUrl: string;
  model: string;
  openaiModel?: string;
  deepseekModel?: string;
  providerKind: FlintAiProviderKind;
}

export interface FlintAiSecureJsonData {
  openaiApiKey?: string;
  deepseekApiKey?: string;
}

export interface FlintAiQuery extends DataQuery {}

export interface GenerateChartRequest {
  prompt: string;
  fields: Array<{ name: string; type: string }>;
  dataHint?: string;
  suggestedChartType?: string;
  renderBackend: 'echarts' | 'vegalite' | 'plotly' | 'chartjs';
  chartCatalog: Array<{
    chartType: string;
    channels: string[];
    requiredChannels: string[];
    properties: Array<{
      key: string;
      type: 'continuous' | 'discrete' | 'binary';
      min?: number;
      max?: number;
      options?: unknown[];
    }>;
  }>;
  semanticTypes: string[];
  sampleRows?: Array<Record<string, unknown>>;
  frameSummary?: Array<{
    frameIndex: number;
    refId?: string;
    fields: string[];
  }>;
  conversation?: ChatMessage[];
}

export interface GenerateChartResponse {
  chartType: string;
  xField?: string;
  yField?: string;
  colorField?: string;
  chartInput?: unknown;
  specJson?: string;
  rationale?: string;
}

export interface RepairChartRequest {
  request: GenerateChartRequest;
  candidate: GenerateChartResponse;
  compileError: string;
  attempt: number;
}

export interface ChatMessage {
  role: 'user' | 'assistant';
  content: string;
}

export interface ChatRequest {
  messages: ChatMessage[];
  /** Flint Panel editor schema — existing callers. */
  panelContext?: {
    fields: Array<{ name: string; type: string }>;
    dataHint?: string;
    renderBackend?: GenerateChartRequest['renderBackend'];
  };
  /** Optional bounded Grafana panel identity for the App chat entry. */
  panelRef?: {
    panelId?: number;
    pluginId?: string;
    title?: string;
    timeFrom?: string;
    timeTo?: string;
    timeZone?: string;
    dashboardUid?: string;
  };
}

export interface ChatResponse {
  message: string;
}

export interface ModelsResponse {
  models: string[];
}

export interface ModelsPreviewRequest {
  providerKind: FlintAiProviderKind;
  baseUrl: string;
  apiKey: string;
}

export interface TestConnectionRequest {
  providerKind: FlintAiProviderKind;
  baseUrl: string;
  model: string;
  apiKey?: string;
}

export interface TestConnectionResponse {
  ok: boolean;
  message: string;
}
