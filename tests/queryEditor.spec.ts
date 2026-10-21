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

test('check summary table returned', async ({ page, panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');

  await row.getByRole('combobox', { name: 'Query Type' }).click();
  await page.getByRole('option', { name: 'Summary Tables' }).click();
  await row.getByRole('combobox', { name: 'Root object' }).click();
  await page.getByRole('option', { name: 'Entire Network', exact: true }).click();
  await row.getByRole('combobox', { name: 'Summary table' }).click();
  await page.getByRole('option', { name: 'Server queues' }).click();

  const response = await panelEditPage.refreshPanel();
  await expect(response).toBeOK();
  const body = await response.json();
  expect(body.results.A.error).toBeUndefined();
  // Queue values stay empty until the server's first poll, so only the node column is checked
  expect(body.results.A.frames).toEqual([
    expect.objectContaining({
      schema: expect.objectContaining({
        name: 'summary-table',
        fields: ['Node', 'Data collector', 'Poller', 'DB writer', 'Event processor'].map((name) =>
          expect.objectContaining({ name })
        ),
      }),
      data: { values: expect.arrayContaining([['e6248c157ee8']]) },
    }),
  ]);
});

test('check object query returned', async ({ page, panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');

  await row.getByRole('combobox', { name: 'Query Type' }).click();
  await page.getByRole('option', { name: 'Object Queries' }).click();
  await row.getByRole('combobox', { name: 'Object query' }).click();
  await page.getByRole('option', { name: 'Nodes' }).click();

  const response = await panelEditPage.refreshPanel();
  await expect(response).toBeOK();
  const body = await response.json();
  expect(body.results.A.error).toBeUndefined();
  expect(body.results.A.frames).toEqual([
    expect.objectContaining({
      schema: expect.objectContaining({
        name: 'object-query',
        fields: ['IP address', 'Host name', 'Status'].map((name) => expect.objectContaining({ name })),
      }),
      data: { values: [['127.0.0.1'], ['127.0.0.1'], [expect.any(String)]] },
    }),
  ]);
});

test('check object query input fields are sent', async ({ page, panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  const row = panelEditPage.getQueryEditorRow('A');

  await row.getByRole('combobox', { name: 'Query Type' }).click();
  await page.getByRole('option', { name: 'Object Queries' }).click();
  await row.getByRole('combobox', { name: 'Object query' }).click();
  await page.getByRole('option', { name: 'Objects by name' }).click();
  await row.getByRole('textbox', { name: 'Query parameters' }).fill('{"pattern": "172.*"}');

  // The query matches nothing without a pattern, so subnet rows show that inputFields reached NXSL
  const response = await panelEditPage.refreshPanel();
  await expect(response).toBeOK();
  const body = await response.json();
  expect(body.results.A.error).toBeUndefined();
  const frame = body.results.A.frames[0];
  expect(frame.schema.fields.map((f: { name: string }) => f.name)).toEqual(['Class', 'Status']);
  expect(frame.data.values[0].length).toBeGreaterThan(0);
  expect(new Set(frame.data.values[0])).toEqual(new Set(['Subnet']));
});
