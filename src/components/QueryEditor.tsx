import React, { useState } from 'react';
import { InlineField, Stack, Combobox } from '@grafana/ui';
import { QueryEditorProps } from '@grafana/data';
import { DataSource } from '../datasource';
import { MyDataSourceOptions, MyQuery } from '../types';

type Props = QueryEditorProps<DataSource, MyQuery, MyDataSourceOptions>;

export function QueryEditor({ query, onChange, onRunQuery, datasource }: Props) {
  const [objectList, setObjectList] = useState<Array<{ label: string; value: string }>>([]);
  const [isLoadingObjects, setIsLoadingObjects] = useState(true);
  const [summaryTableList, setSummaryTableList] = useState<Array<{ label: string; value: string }>>([]);
  const [isLoadingSummaryTable, setIsLoadingSummaryTable] = useState(true);
  const [objectQueryList, setObjectQueryList] = useState<Array<{ label: string; value: string }>>([]);
  const [isLoadingObjectQueries, setIsLoadingObjectQueries] = useState(true);
  const [dciList, setDciList] = useState<Array<{ label: string; value: string }>>([]);
  const [isLoadingDcis, setIsLoadingDcis] = useState(true);

  const handleRootObjectChange = (v: { value: string } | null) => {
    onChange({ ...query, 
      sourceObjectId: v?.value,
      dciId: undefined });
      // Load DCI list
      if (query.queryType === 'dciValues' && v !== null && v.value !== undefined)
      {
        datasource.getDciList(v.value).then(response => {
          const formattedOptions = response.objects.map((item) => ({
            label: item.name,
            value: item.id.toString(),
          }));
          setDciList(formattedOptions);
          setIsLoadingDcis(false);
        });
      }
      handleOnRunQuery();
  };

  const handleOnRunQuery = (): void => {
    switch (query.queryType) {
      case 'alarms':
        onRunQuery();
        break;
      case 'summaryTables':
        // For summary tables, both sourceObjectId and summaryTableId are required
        if (query.sourceObjectId && query.summaryTableId) {
          onRunQuery();
        }
        break;
      case 'objectQueries':
        // For object queries, objectQueryId is required and queryParameters should be valid JSON
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
        // For DCI values, both sourceObjectId and dciId are required
        if (query.sourceObjectId && query.dciId) {
          onRunQuery();
        }
        break;
      default:
        break;
    }
  };

  const onTypeChange = (option: { value: string } | null) => {
    if (option) {
      onChange({ ...query, 
        queryType: option.value,
        sourceObjectId: undefined,
        dciId: undefined,
        summaryTableId: undefined,
        objectQueryId: undefined, });
      // Switch depending on selected value to load from datasource required data
      switch (option.value) {
        case 'alarms':
          // Load alarm object list
          datasource.getAlarmObjectList().then(response => {
            const formattedOptions = response.objects.map((item) => ({
              label: item.name,
              value: item.id.toString(),
            }));
            setObjectList(formattedOptions);
            setIsLoadingObjects(false);
          });
          onRunQuery();
          break;
        case 'summaryTables':
          // Load summary table list
          datasource.getSummaryTableObjectList().then(response => {
            const formattedOptions = response.objects.map((item) => ({
              label: item.name,
              value: item.id.toString(),
            }));
            setObjectList(formattedOptions);
            setIsLoadingObjects(false);
          });
          datasource.getSummaryTableList().then(response => {
            const formattedOptions = response.objects.map((item) => ({
              label: item.name,
              value: item.id.toString(),
            }));
            setSummaryTableList(formattedOptions);
            setIsLoadingSummaryTable(false);
          });
          break;
        case 'objectQueries':
          // Load object query list
          datasource.getObjectQueryObjectList().then(response => {
            console.log('Object query List Response:', response);
            const formattedOptions = response.objects.map((item) => ({
              label: item.name,
              value: item.id.toString(),
            }));
            console.log('Formatted object query Options:', formattedOptions);
            setObjectList(formattedOptions);
            setIsLoadingObjects(false);
          });
          datasource.getObjectQueryList().then(response => {
            const formattedOptions = response.objects.map((item) => ({
              label: item.name,
              value: item.id.toString(),
            }));
            setObjectQueryList(formattedOptions);
            setIsLoadingObjectQueries(false);
          });
          break;
        case 'dciValues':
          // Load object query list
          datasource.getDciObjectList().then(response => {
            const formattedOptions = response.objects.map((item) => ({
              label: item.name,
              value: item.id.toString(),
            }));
            setObjectList(formattedOptions);
            setIsLoadingObjects(false);
          });
          break;
        default:
          break;
      }
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
      {(query.queryType === 'summaryTables' || query.queryType === 'dciValues') && (
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
