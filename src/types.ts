import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

export interface NetXMSQuery extends DataQuery {
  sourceObjectId?: string;
  dciId?: string;
  summaryTableId?: string;
  objectQueryId?: string;
  queryParameters?: string; // JSON string of query parameters (Values)
}

export const DEFAULT_QUERY: Partial<NetXMSQuery> = {
  sourceObjectId: undefined,
};

export interface DataPoint {
  Time: number;
  Value: number;
}

export interface DataSourceResponse {
  datapoints: DataPoint[];
}

/**
 * These are options configured for each DataSource instance
 */
export interface MyDataSourceOptions extends DataSourceJsonData {
  serverAddress: string;
  apiKey: string;
}

/**
 * Value that is used in the backend, but never sent over HTTP to the frontend
 */
export interface NetXMSSecureJsonData {
  apiKey: string;
}

export interface ObjectToIdList {
  objects: Array<{
    name: string;
    id: number;
  }>;
}
