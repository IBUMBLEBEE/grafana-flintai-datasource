import React from 'react';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { DataSourceSettings } from '@grafana/data';

import { ConfigEditor } from './ConfigEditor';
import { FlintAiJsonData, FlintAiSecureJsonData } from './types';

const getMock = jest.fn();
const postMock = jest.fn();

jest.mock('@grafana/runtime', () => ({
  getBackendSrv: () => ({
    get: (...args: unknown[]) => getMock(...args),
    post: (...args: unknown[]) => postMock(...args),
  }),
  isFetchError: (error: unknown): error is { status: number; data: string; statusText: string } =>
    typeof error === 'object' && error !== null && 'status' in error,
}));

jest.mock('@grafana/ui', () => {
  const actual = jest.requireActual('@grafana/ui');
  return {
    ...actual,
    Combobox: ({
      value,
      onChange,
      onIsOpenChange,
      options,
      'aria-labelledby': ariaLabelledBy,
      id,
    }: {
      value?: string | null;
      onChange: (option: { value: string } | null) => void;
      onIsOpenChange?: (isOpen: boolean) => void;
      options: Array<{ label?: string; value: string }>;
      'aria-labelledby'?: string;
      id?: string;
    }) => (
      <div>
        <input
          id={id}
          aria-labelledby={ariaLabelledBy}
          value={value ?? ''}
          onFocus={() => onIsOpenChange?.(true)}
          onChange={(event) => onChange({ value: event.currentTarget.value })}
        />
        <div aria-label="Model options">
          {options.map((option) => (
            <span key={option.value}>{option.label ?? option.value}</span>
          ))}
        </div>
      </div>
    ),
  };
});

function buildOptions(
  overrides: Partial<DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>> = {}
): DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData> {
  return {
    id: 1,
    uid: 'flint-ai',
    orgId: 1,
    name: 'Flint AI',
    typeLogoUrl: '',
    type: 'ibumblebee-flintai-datasource',
    typeName: 'Flint AI',
    access: 'proxy',
    url: '',
    user: '',
    database: '',
    basicAuth: false,
    basicAuthUser: '',
    isDefault: false,
    jsonData: {
      baseUrl: 'https://api.openai.com/v1',
      model: 'saved-model',
      providerKind: 'openai',
    },
    secureJsonFields: { openaiApiKey: true },
    readOnly: false,
    withCredentials: false,
    version: 1,
    ...overrides,
  } as DataSourceSettings<FlintAiJsonData, FlintAiSecureJsonData>;
}

describe('ConfigEditor model discovery', () => {
  beforeEach(() => {
    getMock.mockReset();
    postMock.mockReset();
  });

  it('shows the API key before model selection', () => {
    render(<ConfigEditor options={buildOptions()} onOptionsChange={jest.fn()} />);

    const apiKeyLabel = screen.getByText(/^API key/);
    const modelLabel = screen.getByText('Model');

    expect(apiKeyLabel.compareDocumentPosition(modelLabel)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });

  it('loads saved models when the model combobox opens without sending an apiKey', async () => {
    getMock.mockResolvedValue({ models: ['gpt-b', 'gpt-a'] });
    const onOptionsChange = jest.fn();

    render(<ConfigEditor options={buildOptions()} onOptionsChange={onOptionsChange} />);

    fireEvent.focus(screen.getByRole('textbox', { name: 'Model' }));

    await waitFor(() => {
      expect(getMock).toHaveBeenCalledWith('/api/datasources/uid/flint-ai/resources/models');
    });
    expect(JSON.stringify(getMock.mock.calls)).not.toContain('apiKey');
    expect(postMock).not.toHaveBeenCalled();
    expect(await screen.findByText('gpt-a')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Load models' })).not.toBeInTheDocument();
    expect(screen.queryByText('From catalog')).not.toBeInTheDocument();
  });

  it('keeps the saved model when catalog loading fails', async () => {
    getMock.mockRejectedValue({ status: 404, data: 'Model list is unavailable', statusText: 'Not Found' });
    const options = buildOptions();
    const onOptionsChange = jest.fn();

    render(<ConfigEditor options={options} onOptionsChange={onOptionsChange} />);
    fireEvent.focus(screen.getByRole('textbox', { name: 'Model' }));

    expect(await screen.findByText(/enter a model ID/i)).toBeInTheDocument();
    expect(onOptionsChange).not.toHaveBeenCalled();
    const model = screen.getByRole('textbox', { name: 'Model' });
    expect(model).toHaveValue('saved-model');

    fireEvent.blur(model);
    fireEvent.focus(model);
    fireEvent.change(model, { target: { value: 'fallback-model' } });

    expect(getMock).toHaveBeenCalledTimes(1);
    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({ jsonData: expect.objectContaining({ model: 'fallback-model' }) })
    );
  });

  it('writes a model selected from the loaded provider catalog', async () => {
    getMock.mockResolvedValue({ models: ['gpt-b', 'gpt-a'] });
    const onOptionsChange = jest.fn();

    render(<ConfigEditor options={buildOptions()} onOptionsChange={onOptionsChange} />);
    const model = screen.getByRole('textbox', { name: 'Model' });
    fireEvent.focus(model);
    expect(await screen.findByText('gpt-a')).toBeInTheDocument();
    fireEvent.change(model, { target: { value: 'gpt-a' } });

    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        jsonData: expect.objectContaining({ model: 'gpt-a' }),
      })
    );
  });

  it('accepts a custom model ID typed into the text field', () => {
    const onOptionsChange = jest.fn();
    render(
      <ConfigEditor
        options={buildOptions({
          jsonData: { baseUrl: 'https://api.openai.com/v1', model: '', providerKind: 'openai' },
        })}
        onOptionsChange={onOptionsChange}
      />
    );

    fireEvent.change(screen.getByRole('textbox', { name: 'Model' }), { target: { value: 'deepseek-flash' } });

    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        jsonData: expect.objectContaining({ model: 'deepseek-flash' }),
      })
    );
  });

  it('validates custom model IDs before they are saved', () => {
    const initialOptions = buildOptions({
      jsonData: { baseUrl: 'https://api.openai.com/v1', model: '', providerKind: 'openai' },
    });

    const StatefulEditor = () => {
      const [currentOptions, setCurrentOptions] = React.useState(initialOptions);
      return <ConfigEditor options={currentOptions} onOptionsChange={setCurrentOptions} />;
    };

    render(<StatefulEditor />);
    const model = screen.getByRole('textbox', { name: 'Model' });

    fireEvent.change(model, { target: { value: '   ' } });
    expect(screen.getByText('Model is required')).toBeInTheDocument();

    fireEvent.change(model, { target: { value: 'm'.repeat(257) } });
    expect(screen.getByText('Model ID must be 256 bytes or fewer')).toBeInTheDocument();
  });

  it('tests the current model with the saved API key without exposing the key', async () => {
    postMock.mockResolvedValue({ ok: true, message: 'AI connection succeeded' });

    render(<ConfigEditor options={buildOptions()} onOptionsChange={jest.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Test AI connection' }));

    await waitFor(() => {
      expect(postMock).toHaveBeenCalledWith('/api/datasources/uid/flint-ai/resources/test', {
        providerKind: 'openai',
        baseUrl: 'https://api.openai.com/v1',
        model: 'saved-model',
      });
    });
    expect(JSON.stringify(postMock.mock.calls)).not.toContain('apiKey');
    expect(await screen.findByRole('status')).toHaveTextContent(/^AI connection succeeded$/);
  });

  it('tests unsaved connection settings with the pending API key', async () => {
    postMock.mockResolvedValue({ ok: true, message: 'AI connection succeeded' });
    render(
      <ConfigEditor
        options={buildOptions({ secureJsonFields: {}, secureJsonData: { openaiApiKey: 'new-key' } })}
        onOptionsChange={jest.fn()}
      />
    );

    fireEvent.click(screen.getByRole('button', { name: 'Test AI connection' }));

    await waitFor(() => {
      expect(postMock).toHaveBeenCalledWith('/api/datasources/uid/flint-ai/resources/test', {
        providerKind: 'openai',
        baseUrl: 'https://api.openai.com/v1',
        model: 'saved-model',
        apiKey: 'new-key',
      });
    });
  });

  it('shows a provider error when the AI connection test fails', async () => {
    postMock.mockRejectedValue({
      status: 502,
      data: 'AI provider returned HTTP 401: invalid credentials',
      statusText: 'Bad Gateway',
    });

    render(<ConfigEditor options={buildOptions()} onOptionsChange={jest.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Test AI connection' }));

    expect(await screen.findByText('AI provider returned HTTP 401: invalid credentials')).toBeInTheDocument();
    expect(screen.getByText('AI connection failed')).toBeInTheDocument();
  });

  it('requires the API key before testing an unsaved Base URL', async () => {
    const StatefulEditor = () => {
      const [currentOptions, setCurrentOptions] = React.useState(buildOptions());
      return <ConfigEditor options={currentOptions} onOptionsChange={setCurrentOptions} />;
    };

    render(<StatefulEditor />);
    fireEvent.change(screen.getByRole('textbox', { name: /^Base URL/ }), {
      target: { value: 'https://gateway.example.test/v1' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Test AI connection' }));

    expect(await screen.findByText(/Enter the API key to test unsaved Provider or Base URL changes/i)).toBeInTheDocument();
    expect(postMock).not.toHaveBeenCalled();
  });

  it('loads models with an unsaved API key through the preview resource', async () => {
    postMock.mockResolvedValue({ models: ['preview-model'] });
    render(
      <ConfigEditor
        options={buildOptions({ secureJsonFields: {}, secureJsonData: { openaiApiKey: 'new-key' } })}
        onOptionsChange={jest.fn()}
      />
    );

    fireEvent.focus(screen.getByRole('textbox', { name: 'Model' }));

    await waitFor(() => {
      expect(postMock).toHaveBeenCalledWith('/api/datasources/uid/flint-ai/resources/models/preview', {
        providerKind: 'openai',
        baseUrl: 'https://api.openai.com/v1',
        apiKey: 'new-key',
      });
    });
    expect(getMock).not.toHaveBeenCalled();
    expect(await screen.findByText('preview-model')).toBeInTheDocument();
  });

  it('asks for connection details when neither a pending nor saved key is available', async () => {
    render(
      <ConfigEditor
        options={buildOptions({ secureJsonFields: {}, secureJsonData: {} })}
        onOptionsChange={jest.fn()}
      />
    );

    fireEvent.focus(screen.getByRole('textbox', { name: 'Model' }));

    expect(await screen.findByText(/Enter a Base URL and API key/i)).toBeInTheDocument();
    expect(getMock).not.toHaveBeenCalled();
    expect(postMock).not.toHaveBeenCalled();
  });

  it('shows an empty model for an unconfigured provider while keeping each API key configured', () => {
    const onOptionsChange = jest.fn();
    render(
      <ConfigEditor
        options={buildOptions({
          secureJsonFields: { openaiApiKey: true, deepseekApiKey: true },
        })}
        onOptionsChange={onOptionsChange}
      />
    );

    fireEvent.click(screen.getByRole('radio', { name: 'DeepSeek' }));

    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        jsonData: expect.objectContaining({
          providerKind: 'deepseek',
          baseUrl: 'https://api.deepseek.com/v1',
          model: '',
        }),
        secureJsonFields: expect.objectContaining({ openaiApiKey: true, deepseekApiKey: true }),
      })
    );
  });

  it('restores the selected model when switching back to a provider', () => {
    const initialOptions = buildOptions({
      jsonData: {
        providerKind: 'deepseek',
        baseUrl: 'https://api.deepseek.com/v1',
        model: 'deepseek-chat',
      },
      secureJsonFields: { openaiApiKey: true, deepseekApiKey: true },
    });

    const StatefulEditor = () => {
      const [currentOptions, setCurrentOptions] = React.useState(initialOptions);
      return <ConfigEditor options={currentOptions} onOptionsChange={setCurrentOptions} />;
    };

    render(<StatefulEditor />);
    fireEvent.click(screen.getByRole('radio', { name: 'OpenAI' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Model' }), { target: { value: 'gpt-5' } });
    fireEvent.click(screen.getByRole('radio', { name: 'DeepSeek' }));

    expect(screen.getByRole('textbox', { name: 'Model' })).toHaveValue('deepseek-chat');
  });

  it('asks the user to confirm a custom Base URL after switching provider', () => {
    const onOptionsChange = jest.fn();
    render(
      <ConfigEditor
        options={buildOptions({
          jsonData: {
            providerKind: 'openai',
            baseUrl: 'https://gateway.example.test/v1',
            model: 'custom-model',
          },
        })}
        onOptionsChange={onOptionsChange}
      />
    );

    fireEvent.click(screen.getByRole('radio', { name: 'DeepSeek' }));

    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        jsonData: expect.objectContaining({
          providerKind: 'deepseek',
          baseUrl: 'https://gateway.example.test/v1',
          model: '',
        }),
      })
    );
    expect(screen.getByText(/Confirm that this custom Base URL supports DeepSeek/i)).toBeInTheDocument();
  });

  it('does not silently use OpenAI for an unsupported saved provider', () => {
    const options = buildOptions();
    options.jsonData = {
      ...options.jsonData,
      providerKind: 'gemini',
    } as unknown as FlintAiJsonData;

    render(<ConfigEditor options={options} onOptionsChange={jest.fn()} />);

    expect(screen.getByText(/Unsupported provider configuration/i)).toBeInTheDocument();
    fireEvent.focus(screen.getByRole('textbox', { name: 'Model' }));
    expect(screen.getByText(/Enter a Base URL and API key/i)).toBeInTheDocument();
    expect(getMock).not.toHaveBeenCalled();
    expect(postMock).not.toHaveBeenCalled();
  });

  it('persists explicit OpenAI defaults when a new datasource token is entered', () => {
    const onOptionsChange = jest.fn();
    const options = buildOptions({
      uid: '',
      jsonData: {} as FlintAiJsonData,
      secureJsonFields: {},
      secureJsonData: {},
    });

    render(<ConfigEditor options={options} onOptionsChange={onOptionsChange} />);
    fireEvent.change(screen.getByPlaceholderText('OpenAI API key'), { target: { value: 'new-token' } });

    expect(onOptionsChange).toHaveBeenCalledWith(
      expect.objectContaining({
        jsonData: {
          providerKind: 'openai',
          baseUrl: 'https://api.openai.com/v1',
          model: '',
        },
        secureJsonData: { openaiApiKey: 'new-token' },
      })
    );
  });
});
