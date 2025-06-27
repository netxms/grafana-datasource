import { DataSourcePlugin } from '@grafana/data';
import { DataSource } from './datasource';
import { ConfigEditor } from './components/ConfigEditor';
import { QueryEditor } from './components/QueryEditor';
import { NetXMSQuery, NetxmsSourceOptions as NetXMSDataSourceOptions, NetXMSSecureJsonData as NetXMSSecureJsonData } from './types';

export const plugin = new DataSourcePlugin<DataSource, NetXMSQuery, NetXMSDataSourceOptions, NetXMSSecureJsonData>(DataSource)
  .setConfigEditor(ConfigEditor)
  .setQueryEditor(QueryEditor);
