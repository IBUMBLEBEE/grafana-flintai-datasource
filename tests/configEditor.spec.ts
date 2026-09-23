import { test, expect } from '@grafana/plugin-e2e';

const providerCases = [
  { name: 'OpenAI', datasourceName: 'Flint AI - OpenAI Stub', customModel: 'custom-openai-model' },
  { name: 'DeepSeek', datasourceName: 'Flint AI - DeepSeek Stub', customModel: 'custom-deepseek-model' },
] as const;

test.describe('Flint AI provider configuration', () => {
  for (const provider of providerCases) {
    test(`${provider.name} loads models, accepts a custom model, and calls chat`, async ({
      gotoDataSourceConfigPage,
      readProvisionedDataSource,
      page,
    }) => {
      const ds = await readProvisionedDataSource({
        fileName: 'flint-ai.yml',
        name: provider.datasourceName,
      });
      const configPage = await gotoDataSourceConfigPage(ds.uid);

      const resourceCalls: string[] = [];
      page.on('request', (request) => {
        if (request.url().includes('/resources/')) {
          resourceCalls.push(request.url());
        }
      });

      const model = page.getByRole('combobox', { name: 'Model' });
      await expect(model).toBeVisible();
      await model.click();
      await page.getByRole('option', { name: 'stub-chat' }).click();
      await expect(model).toHaveValue('stub-chat');

      const otherProvider = provider.name === 'OpenAI' ? 'DeepSeek' : 'OpenAI';
      await page.getByRole('radio', { name: otherProvider }).click();
      await expect(model).toHaveValue('');
      await page.getByRole('radio', { name: provider.name }).click();
      await expect(model).toHaveValue('stub-chat');

      await model.fill(provider.customModel);
      const customModelOption = page.getByRole('option').filter({ hasText: provider.customModel });
      await expect(customModelOption).toBeVisible();
      await customModelOption.click();
      await expect(model).toHaveValue(provider.customModel);

      const testRequestPromise = page.waitForRequest(
        (request) => request.url().endsWith('/resources/test') && request.method() === 'POST'
      );
      await page.getByRole('button', { name: 'Test AI connection' }).click();
      const testRequest = await testRequestPromise;
      expect(testRequest.postDataJSON()).toMatchObject({
        providerKind: provider.name.toLowerCase(),
        model: provider.customModel,
      });
      expect(testRequest.postDataJSON()).not.toHaveProperty('apiKey');
      await expect(page.getByRole('group', { name: 'AI provider' }).getByText('AI connection succeeded')).toBeVisible();

      await expect(configPage.saveAndTest()).toBeOK();

      const chatResponse = await page.request.post(`/api/datasources/uid/${ds.uid}/resources/chat`, {
        data: { messages: [{ role: 'user', content: `hello from ${provider.name}` }] },
      });
      expect(chatResponse.ok()).toBeTruthy();
      await expect(chatResponse.json()).resolves.toEqual({ message: 'stub' });

      expect(resourceCalls.some((url) => url.includes('/resources/models'))).toBeTruthy();
      expect(resourceCalls.join('\n')).not.toMatch(/apiKey=|token=/i);
      await expect(page.getByRole('button', { name: /reset/i })).toBeVisible();
    });
  }

  test('loads models with an edited API key before saving', async ({
    gotoDataSourceConfigPage,
    readProvisionedDataSource,
    page,
  }) => {
    const ds = await readProvisionedDataSource({
      fileName: 'flint-ai.yml',
      name: 'Flint AI - OpenAI Stub',
    });
    await gotoDataSourceConfigPage(ds.uid);

    await page.getByRole('button', { name: /reset/i }).click();
    await page.getByPlaceholder('OpenAI API key').fill('stub-openai-key');

    const previewRequest = page.waitForRequest(
      (request) => request.url().endsWith('/resources/models/preview') && request.method() === 'POST'
    );
    const model = page.getByRole('combobox', { name: 'Model' });
    await model.click();

    const request = await previewRequest;
    expect(request.postDataJSON()).toEqual({
      providerKind: 'openai',
      baseUrl: 'http://stub-provider:18080/v1',
      apiKey: 'stub-openai-key',
    });
    await page.getByRole('option', { name: 'stub-chat' }).click();
    await expect(model).toHaveValue('stub-chat');

    await expect(page.getByRole('button', { name: 'Load models' })).toHaveCount(0);
    await expect(page.getByText('From catalog')).toHaveCount(0);
  });

  test('shows an authentication error without clearing the configured model', async ({
    gotoDataSourceConfigPage,
    readProvisionedDataSource,
    page,
  }) => {
    const ds = await readProvisionedDataSource({
      fileName: 'flint-ai.yml',
      name: 'Flint AI - Authentication Error Stub',
    });
    await gotoDataSourceConfigPage(ds.uid);

    const model = page.getByRole('combobox', { name: 'Model' });
    await model.click();

    await expect(page.getByText(/Authentication failed; check the API key/i)).toBeVisible();
    await expect(model).toHaveValue('manual-model');
  });

  test('keeps manual model entry available when the models endpoint is unavailable', async ({
    gotoDataSourceConfigPage,
    readProvisionedDataSource,
    page,
  }) => {
    const ds = await readProvisionedDataSource({
      fileName: 'flint-ai.yml',
      name: 'Flint AI - Unavailable Models Stub',
    });
    const configPage = await gotoDataSourceConfigPage(ds.uid);

    const model = page.getByRole('combobox', { name: 'Model' });
    await model.click();

    await expect(page.getByText(/Model catalog is unavailable for this Base URL/i)).toBeVisible();
    await expect(model).toHaveValue(/.+/);
    await model.fill('replacement-custom-model');
    await model.press('Enter');
    await expect(model).toHaveValue('replacement-custom-model');
    await expect(configPage.saveAndTest()).toBeOK();

    await page.reload();
    await expect(page.getByRole('combobox', { name: 'Model' })).toHaveValue('replacement-custom-model');
  });
});
