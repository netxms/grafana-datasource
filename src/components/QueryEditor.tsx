import React, { useState, useEffect, useCallback } from 'react';
import { InlineField, Stack, Combobox } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';
import { DataSource } from '../datasource';
import { NetxmsSourceOptions as NetXMSDataSourceOptions, NetXMSQuery } from '../types';

type Props = QueryEditorProps<DataSource, NetXMSQuery, NetXMSDataSourceOptions>;

type Option = { label: string; value: string };

export function QueryEditor({ query, onChange, onRunQuery, datasource }: Props) {
  const [objectList, setObjectList] = useState<Option[]>([]);
  const [isLoadingObjects, setIsLoadingObjects] = useState(true);
  const [summaryTableList, setSummaryTableList] = useState<Option[]>([]);
  const [isLoadingSummaryTable, setIsLoadingSummaryTable] = useState(true);
  const [objectQueryList, setObjectQueryList] = useState<Option[]>([]);
  const [isLoadingObjectQueries, setIsLoadingObjectQueries] = useState(true);
  const [dciList, setDciList] = useState<Option[]>([]);
  const [isLoadingDcis, setIsLoadingDcis] = useState(true);

  const formatOptions = useCallback((response: any): Option[] => {
    return response.objects.map((item: any) => ({
      label: item.name,
      value: item.id.toString(),
    }));
  }, []);

  const loadObjectList = useCallback(async (type: string) => {
    setIsLoadingObjects(true);
    try {
      let response;
      switch (type) {
        case 'alarms':
          response = await datasource.getAlarmObjectList();
          break;
        case 'objectStatus':
        case 'summaryTables':
          response = await datasource.getSummaryTableObjectList();
          break;
        case 'objectQueries':
          response = await datasource.getObjectQueryObjectList();
          break;
        case 'dciValues':
          response = await datasource.getDciObjectList();
          break;
        default:
          return;
      }
      setObjectList(formatOptions(response));
    } finally {
      setIsLoadingObjects(false);
    }
  }, [datasource, formatOptions]);

  const loadDciList = useCallback(async (objectId: string) => {
    setIsLoadingDcis(true);
    try {
      const response = await datasource.getDciList(objectId);
      setDciList(formatOptions(response));
    } finally {
      setIsLoadingDcis(false);
    }
  }, [datasource, formatOptions]);

  const loadSummaryTableList = useCallback(async () => {
    setIsLoadingSummaryTable(true);
    try {
      const response = await datasource.getSummaryTableList();
      setSummaryTableList(formatOptions(response));
    } finally {
      setIsLoadingSummaryTable(false);
    }
  }, [datasource, formatOptions]);

  const loadObjectQueryList = useCallback(async () => {
    setIsLoadingObjectQueries(true);
    try {
      const response = await datasource.getObjectQueryList();
      setObjectQueryList(formatOptions(response));
    } finally {
      setIsLoadingObjectQueries(false);
    }
  }, [datasource, formatOptions]);

  // Load required elements on mount if query type is set
  useEffect(() => {
    if (!query.queryType) {
      return;
    }

    switch (query.queryType) {
      case 'alarms':
        loadObjectList('alarms');
        break;
      case 'summaryTables':
        loadObjectList('summaryTables');
        loadSummaryTableList();
        break;
      case 'objectQueries':
        loadObjectList('objectQueries');
        loadObjectQueryList();
        break;
      case 'dciValues':
        loadObjectList('dciValues');
        if (query.sourceObjectId) {
          loadDciList(query.sourceObjectId);
        }
        break;
      case 'objectStatus':
        loadObjectList('summaryTables');
        break;
    }
  }, [query.queryType, query.sourceObjectId, loadObjectList, loadSummaryTableList, loadObjectQueryList, loadDciList]);

  const handleRootObjectChange = (v: { value: string } | null) => {
    onChange({ ...query, 
      sourceObjectId: v?.value,
      dciId: undefined });
    
    if (query.queryType === 'dciValues' && v?.value) {
      loadDciList(v.value);
    }
    handleOnRunQuery();
  };

  const handleOnRunQuery = (): void => {
    switch (query.queryType) {
      case 'alarms':
        onRunQuery();
        break;
      case 'summaryTables':
        if (query.sourceObjectId && query.summaryTableId) {
          onRunQuery();
        }
        break;
      case 'objectQueries':
        if (query.objectQueryId) {
          if (query.queryParameters) {
            try {
              JSON.parse(query.queryParameters);
              onRunQuery();
            } catch (err) {
              // Invalid JSON, don't run the query
            }
          } else {
            onRunQuery();
          }
        }
        break;
      case 'dciValues':
        if (query.sourceObjectId && query.dciId) {
          onRunQuery();
        }
        break;
      case 'objectStatus':
        if (query.sourceObjectId) {
          onRunQuery();
        }
        break;
    }
  };

  const onTypeChange = (option: { value: string } | null) => {
    if (!option) {
      return;
    }

    onChange({ ...query, 
      queryType: option.value,
      sourceObjectId: undefined,
      dciId: undefined,
      summaryTableId: undefined,
      objectQueryId: undefined,
    });

    switch (option.value) {
      case 'alarms':
        loadObjectList('alarms');
        onRunQuery();
        break;
      case 'summaryTables':
        loadObjectList('summaryTables');
        loadSummaryTableList();
        break;
      case 'objectQueries':
        loadObjectList('objectQueries');
        loadObjectQueryList();
        break;
      case 'dciValues':
        loadObjectList('dciValues');
        break;
      case 'objectStatus':
        loadObjectList('summaryTables');
        break;
    }
  };

  return (
    <Stack gap={2} direction="column">
      <InlineField label="Query Type">
        <Combobox
          value={query.queryType}
          options={[
            { label: 'Alarms', value: 'alarms' },
            { label: 'Summary Tables', value: 'summaryTables' },
            { label: 'Object Queries', value: 'objectQueries' },
            { label: 'DCI value', value: 'dciValues' },
            { label: 'Object Status', value: 'objectStatus' },
          ]}         
          onChange={ onTypeChange }
        />
      </InlineField>

      {/* Optional object selector */}
      {(query.queryType === 'alarms' || query.queryType === 'objectQueries') && (
        <InlineField label="Root object" labelWidth={16}>
          <Combobox
            value={query.sourceObjectId}
            isClearable={true}
            onChange={ (v) => { onChange({ ...query, sourceObjectId: v?.value }); handleOnRunQuery(); }}  
            options={objectList}
            loading={isLoadingObjects}
            placeholder="Root object"
            width={32}
          />
        </InlineField>
      )}

      {/* Required object selector */}
      {(query.queryType === 'summaryTables' || query.queryType === 'dciValues' || query.queryType === 'objectStatus') && (
        <InlineField label="Root object" labelWidth={16}>
          <Combobox
            value={query.sourceObjectId}
            onChange={handleRootObjectChange}
            options={objectList}
            loading={isLoadingObjects}
            placeholder="Root object"
            width={32}
          />
        </InlineField>
      )}


      {query.queryType === 'summaryTables' && (        
        <InlineField label="Summary table" labelWidth={16}>
          <Combobox
            value={query.summaryTableId}
            onChange={ (v) => { onChange({ ...query, summaryTableId: v.value }); handleOnRunQuery(); }}       
            options={summaryTableList}
            loading={isLoadingSummaryTable}
            placeholder="Summary table"
            width={32}
          />
        </InlineField>
      )}

      {query.queryType === 'objectQueries' && (
        <>
          <InlineField label="Object query" labelWidth={16}>
            <Combobox
              value={query.objectQueryId}
              onChange={ (v) => { onChange({ ...query, objectQueryId: v.value }); handleOnRunQuery(); }}          
              options={objectQueryList}
              loading={isLoadingObjectQueries}
              placeholder="Select query"
              width={32}
            />
          </InlineField>
          <InlineField label="Query parameters" labelWidth={16}>
            <textarea
              value={query.queryParameters || ''}
              onChange={(e) => { 
                try {
                  JSON.parse(e.target.value); // Just validate JSON
                  onChange({ ...query, queryParameters: e.target.value }); 
                  handleOnRunQuery();
                } catch (err) {
                  // If JSON is invalid, just update the value without running the query
                  onChange({ ...query, queryParameters: e.target.value });
                }
              }}
              placeholder="Enter query parameters as JSON array of key-value pairs"
              style={{ width: '32em', height: '5em' }}
            />
          </InlineField>
        </>
      )}

      {query.queryType === 'dciValues' && (
        <InlineField label="DCI" labelWidth={16}>
          <Combobox
            value={query.dciId}
            onChange={ (v) => { onChange({ ...query, dciId: v.value }); handleOnRunQuery(); }}  
            options={dciList}
            loading={isLoadingDcis}
            width={32}
          />
        </InlineField>
      )}
    </Stack>
  );
}
