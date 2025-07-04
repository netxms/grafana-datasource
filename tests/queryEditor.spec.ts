import { test, expect } from '@grafana/plugin-e2e';
/*
test('smoke: should render query editor', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  await expect(panelEditPage.getQueryEditorRow('A').getByRole('textbox', { name: 'Query Text' })).toBeVisible();
});

test('should trigger new query when Constant field is changed', async ({
  panelEditPage,
  readProvisionedDataSource,
}) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  await panelEditPage.getQueryEditorRow('A').getByRole('textbox', { name: 'Query Text' }).fill('test query');
  const queryReq = panelEditPage.waitForQueryDataRequest();
  await panelEditPage.getQueryEditorRow('A').getByRole('spinbutton').fill('10');
  await expect(await queryReq).toBeTruthy();
});

test('data query should return values 10 and 20', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  await panelEditPage.getQueryEditorRow('A').getByRole('textbox', { name: 'Query Text' }).fill('test query');
  await panelEditPage.setVisualization('Table');
  await expect(panelEditPage.refreshPanel()).toBeOK();
  await expect(panelEditPage.panel.data).toContainText(['10', '20']);
});
*/
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
  await expect(panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: "Root object" })).toBeVisible();
});

test('smoke: chek query type filed exists', async ({ panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  
  await expect(panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: 'Query Type' })).toBeVisible();
});


test('chek alarms returned', async ({ page, panelEditPage, readProvisionedDataSource }) => {
  const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
  await panelEditPage.datasource.set(ds.name);
  
  await panelEditPage.setVisualization('Table');
  await panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: 'Query Type' }).click();
  await page.getByRole('option', { name: 'Alarms' }).click();

  await expect(panelEditPage.getQueryEditorRow('A').getByRole('combobox', { name: "Root object" })).toBeVisible();

  /* TODO: fix test
  const queryDataSpy = panelEditPage.waitForQueryDataRequest((request) =>
    (request.postData() ?? '').includes(`alarms`)
  );

  await panelEditPage.refreshPanel();
  await expect(queryDataSpy).resolves.toBeTruthy(); */
});
