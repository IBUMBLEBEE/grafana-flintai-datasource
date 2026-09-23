import { DataSourceInstanceSettings } from '@grafana/data';

import { DataSource } from './datasource';
import { FlintAiJsonData, RepairChartRequest } from './types';

const postResourceMock = jest.fn();

jest.mock('@grafana/runtime', () => ({
  DataSourceWithBackend: class {
    postResource = postResourceMock;
  },
}));

describe('DataSource resource requests', () => {
  beforeEach(() => {
    postResourceMock.mockReset();
    postResourceMock.mockResolvedValue({ message: 'AI response' });
  });

  it('does not turn an AI chat response into a Grafana success notification', async () => {
    const datasource = new DataSource({} as DataSourceInstanceSettings<FlintAiJsonData>);
    const abortController = new AbortController();

    await datasource.chat({ messages: [{ role: 'user', content: 'Explain this chart' }] }, abortController.signal);

    expect(postResourceMock).toHaveBeenCalledWith(
      '/chat',
      { messages: [{ role: 'user', content: 'Explain this chart' }] },
      { abortSignal: abortController.signal, showSuccessAlert: false }
    );
  });

  it('sends bounded repair requests through the dedicated resource without a success notification', async () => {
    const datasource = new DataSource({} as DataSourceInstanceSettings<FlintAiJsonData>);
    const abortController = new AbortController();
    const input: RepairChartRequest = {
      request: {
        prompt: 'rounded bars',
        fields: [{ name: 'value', type: 'number' }],
        renderBackend: 'echarts',
        chartCatalog: [],
        semanticTypes: ['Quantity'],
      },
      candidate: { chartType: 'Bar Chart' },
      compileError: 'cornerRadius must be between 0 and 15',
      attempt: 1,
    };

    await datasource.repair(input, abortController.signal);

    expect(postResourceMock).toHaveBeenCalledWith('/repair', input, {
      abortSignal: abortController.signal,
      showSuccessAlert: false,
    });
  });
});
