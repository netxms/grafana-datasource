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
  console.log('serverAddress:', ds?.jsonData?.serverAddress);

  // Connectivity check
  if (ds?.jsonData?.serverAddress) {
    try {
      const response = await fetch(ds.jsonData.serverAddress);
      console.log('Connectivity check status:', response.status);
    } catch (err) {
      console.error('Connectivity check failed:', err);
    }
  }

  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await page.getByRole('textbox', { name: 'API address' }).fill(ds.jsonData.serverAddress ?? '');
  await page.getByRole('textbox', { name: 'API Key' }).fill(ds.secureJsonData?.apiKey ?? '');
  const result = await configPage.saveAndTest();
  expect(result.ok()).toBeTruthy();
});

test('"Save & test" should fail when configuration is invalid', async ({
  createDataSourceConfigPage,
  readProvisionedDataSource,
  page,
}) => {
  const ds = await readProvisionedDataSource<NetxmsSourceOptions, NetXMSSecureJsonData>({ fileName: 'datasources.yml' });
  const configPage = await createDataSourceConfigPage({ type: ds.type });
  await page.getByRole('textbox', { name: 'API address' }).fill(ds.jsonData.serverAddress ?? '');
  await expect(configPage.saveAndTest()).not.toBeOK();
  await expect(configPage).toHaveAlert('error', { hasText: 'API key is missing' });
});
