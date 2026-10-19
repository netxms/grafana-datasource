import { DataSourceInstanceSettings, CoreApp } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';

import { NetXMSQuery, NetxmsSourceOptions as NetXMSDataSourceOptions, DEFAULT_QUERY, ObjectToIdList } from './types';

// Errors are shown inside the query editor, so suppress Grafana's global error toast
const RESOURCE_OPTIONS = { showErrorAlert: false };

export class DataSource extends DataSourceWithBackend<NetXMSQuery, NetXMSDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<NetXMSDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<NetXMSQuery> {
    return DEFAULT_QUERY;
  }

  getAlarmObjectList(): Promise<ObjectToIdList> {
    return this.getResource('alarmObjects', undefined, RESOURCE_OPTIONS);
  }

  getSummaryTableObjectList(): Promise<ObjectToIdList> {
    return this.getResource('summaryTableObjects', undefined, RESOURCE_OPTIONS);
  }

  getObjectQueryObjectList(): Promise<ObjectToIdList> {
    return this.getResource('objectQueryObjects', undefined, RESOURCE_OPTIONS);
  }

  getObjectQueryList(): Promise<ObjectToIdList> {
    return this.getResource('objectQueries', undefined, RESOURCE_OPTIONS);
  }

  getDciObjectList(): Promise<ObjectToIdList> {
    return this.getResource('dciObjects', undefined, RESOURCE_OPTIONS);
  }

  getDciList(objectId: string): Promise<ObjectToIdList> {
    return this.getResource('dcis', { objectId }, RESOURCE_OPTIONS);
  }

  getSummaryTableList(): Promise<ObjectToIdList> {
    return this.getResource('summaryTables', undefined, RESOURCE_OPTIONS);
  }

  filterQuery(query: NetXMSQuery): boolean {
    if (!query.queryType) {
      return false;
    }

    switch (query.queryType) {
      case 'alarms':
        // No required fields for alarms
        return true;

      case 'dciValues':
        // Both sourceObjectId and dciId are required
        return !!(query.sourceObjectId && query.dciId);

      case 'summaryTables':
        // Both sourceObjectId and summaryTableId are required
        return !!(query.sourceObjectId && query.summaryTableId);

      case 'objectQueries':
        // objectQueryId is required; sourceObjectId is optional
        // queryParameters is optional but must be valid JSON if provided
        if (!query.objectQueryId) {
          return false;
        }
        if (query.queryParameters) {
          try {
            JSON.parse(query.queryParameters);
          } catch (e) {
            return false;
          }
        }
        return true;
      case 'objectStatus':
        // sourceObjectId is required
        return !!query.sourceObjectId;
      default:
        return false;
    }
  }
}
