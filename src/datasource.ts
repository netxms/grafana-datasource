import { DataSourceInstanceSettings, CoreApp } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';

import { MyQuery, MyDataSourceOptions, DEFAULT_QUERY, ObjectToIdList } from './types';

export class DataSource extends DataSourceWithBackend<MyQuery, MyDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<MyDataSourceOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<MyQuery> {
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
}
