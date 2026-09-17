import { DataQuery, DataSourceJsonData } from '@grafana/data';

export const FLINT_AI_DATASOURCE_PLUGIN_ID = 'ibumblebee-flint-ai-datasource';
export const FLINT_AI_PROVIDER_KINDS = ['openai', 'deepseek'] as const;
export type FlintAiProviderKind = (typeof FLINT_AI_PROVIDER_KINDS)[number];
export const DEFAULT_FLINT_AI_PROVIDER_KIND: FlintAiProviderKind = 'openai';

export interface FlintAiJsonData extends DataSourceJsonData {
  baseUrl: string;
  model: string;
  providerKind: FlintAiProviderKind;
}

export interface FlintAiSecureJsonData {
  apiKey?: string;
}

export interface FlintAiQuery extends DataQuery {}

export interface GenerateChartRequest {
  prompt: string;
  fields: Array<{ name: string; type: string }>;
  dataHint?: string;
  suggestedChartType?: string;
  renderBackend: 'echarts' | 'vegalite' | 'plotly' | 'chartjs';
  chartCatalog: Array<{ chartType: string; channels: string[] }>;
  frameSummary?: Array<{ frameIndex: number; refId?: string; fields: string[] }>;
  conversation?: ChatMessage[];
}

export interface GenerateChartResponse {
  chartType: string;
  xField?: string;
  yField?: string;
  colorField?: string;
  specJson?: string;
  rationale?: string;
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
