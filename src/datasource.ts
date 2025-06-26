import { DataSourceInstanceSettings, CoreApp } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';

import { NetXMSQuery, MyDataSourceOptions as NetXMSDataSourceOptions, DEFAULT_QUERY, ObjectToIdList } from './types';

export class DataSource extends DataSourceWithBackend<NetXMSQuery, NetXMSDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<NetXMSDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<NetXMSQuery> {
    return DEFAULT_QUERY;
  }
  
  getAlarmObjectList(): Promise<ObjectToIdList> {
    return this.getResource('alarmObjects');
  }
  
  getSummaryTableObjectList(): Promise<ObjectToIdList> {
    return this.getResource('summaryTableObjects');
  }
  
  getObjectQueryObjectList(): Promise<ObjectToIdList> {
    return this.getResource('objectQueryObjects');
  }
  
  getObjectQueryList(): Promise<ObjectToIdList> {
    return this.getResource('objectQueries');
  }

  getDciObjectList(): Promise<ObjectToIdList> {
    return this.getResource('dciObjects');
  }

  getDciList(objectId: string): Promise<ObjectToIdList> {
    return this.getResource('dcis', { name: "objectId", objectId });
  }

  getSummaryTableList(): Promise<ObjectToIdList> {
    return this.getResource('summaryTables');
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
        // sourceObjectId, objectQueryId are required
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
