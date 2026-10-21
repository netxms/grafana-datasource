import { test, expect } from '@grafana/plugin-e2e';

test('testing select component', async ({ readProvisionedDataSource, page, panelEditPage }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);

  await panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: 'Query Type' }).click();
  await expect(page.getByRole('option')).toHaveText([
    'Alarms',
    'Summary Tables',
    'Object Queries',
    'DCI value',
    'Object Status',
  ]);
  await page.getByRole('option', { name: 'Alarms' }).click();
  await expect(panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: 'Root object' })).toBeVisible();
});

test('smoke: check query type field exists', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);

  await expect(panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: 'Query Type' })).toBeVisible();
});

test('check alarms returned', async ({ page, panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);

  await panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: 'Query Type' }).click();
  await page.getByRole('option', { name: 'Alarms' }).click();

  await expect(panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: 'Root object' })).toBeVisible();

  const response = await panelEditPage.refreshPanel();
  await expect(response).toBeOK();
  const body = await response.json();
  expect(body.results.A.error).toBeUndefined();
  expect(body.results.A.frames).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        schema: expect.objectContaining({
          name: 'alarms',
          fields: [
            'Id',
            'Severity',
            'State',
            'Source',
            'Message',
            'Count',
            'Ack/Resolve by',
            'Created',
            'Last Change',
          ].map((name) => expect.objectContaining({ name })),
        }),
      }),
    ])
  );
});
