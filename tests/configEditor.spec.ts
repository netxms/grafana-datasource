import { test, expect } from '@grafana/plugin-e2e';
import { NetxmsSourceOptions, NetXMSSecureJsonData } from '../src/types';

test('smoke: should render config editor', async ({ createDataSourceConfigPage, readProvisionedDataSource, page }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await createDataSourceConfigPage({ type: ds.type });
  await expect(page.getByLabel('API address')).toBeVisible();
});
test('"Save & test" should be successful when configuration is valid', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource<NetxmsSourceOptions, NetXMSSecureJsonData>({ fileName: 'datasources.yml' });
  const apiKey = ds.secureJsonData?.apiKey ?? '';
  const serverAddress = ds.jsonData.serverAddress ?? '';
  console.log('serverAddress:', serverAddress);
  console.log('apiKey length:', apiKey.length);

  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await page.getByPlaceholder('Enter the address').fill(serverAddress);
  await page.getByPlaceholder('Enter your API key').fill(apiKey);
  const result = await configPage.saveAndTest();
  console.error('Save and test result:', result);
  expect(result.ok()).toBeTruthy();
});

test('"Save & test" should fail when configuration is invalid', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource<NetxmsSourceOptions, NetXMSSecureJsonData>({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await page.getByPlaceholder('Enter the address').fill(ds.jsonData.serverAddress ?? '');
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'API key is missing' });
});
