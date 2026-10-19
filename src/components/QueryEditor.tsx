import React, { useState, useEffect, useCallback } from 'react';
import { Alert, InlineField, Stack, Combobox, Select } from '@grafana/ui';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { DataSource } from '../datasource';
import { NetxmsSourceOptions as NetXMSDataSourceOptions, NetXMSQuery, ObjectToIdList } from '../types';

type Props = QueryEditorProps<DataSource, NetXMSQuery, NetXMSDataSourceOptions>;

type Option = SelectableValue<string>;

function formatOptions(response: ObjectToIdList | undefined): Option[] {
  return (response?.objects ?? []).map((item) => ({
    label: item.name,
    value: item.id.toString(),
  }));
}

function errorMessage(err: unknown): string {
  const e = err as { data?: { reason?: string; message?: string }; statusText?: string; message?: string };
  return e?.data?.reason || e?.data?.message || e?.statusText || e?.message || 'Unknown error';
}

export function QueryEditor({ query, onChange, onRunQuery, datasource }: Props) {
  const [objectList, setObjectList] = useState<Option[]>([]);
  const [isLoadingObjects, setIsLoadingObjects] = useState(false);
  const [summaryTableList, setSummaryTableList] = useState<Option[]>([]);
  const [isLoadingSummaryTable, setIsLoadingSummaryTable] = useState(false);
  const [objectQueryList, setObjectQueryList] = useState<Option[]>([]);
  const [isLoadingObjectQueries, setIsLoadingObjectQueries] = useState(false);
  const [dciList, setDciList] = useState<Option[]>([]);
  const [isLoadingDcis, setIsLoadingDcis] = useState(false);
  const [listError, setListError] = useState<string | undefined>();
  const [dciError, setDciError] = useState<string | undefined>();

  // Loads one dropdown list. Results of a request that was superseded (query type or root
  // object changed meanwhile) are dropped via isCancelled.
  const load = useCallback(
    async (
      fetch: () => Promise<ObjectToIdList>,
      setList: (options: Option[]) => void,
      setLoading: (loading: boolean) => void,
      setError: (error: string | undefined) => void,
      isCancelled: () => boolean
    ) => {
      setLoading(true);
      try {
        const response = await fetch();
        if (!isCancelled()) {
          setList(formatOptions(response));
        }
      } catch (err) {
        if (!isCancelled()) {
          setList([]);
          setError(errorMessage(err));
        }
      } finally {
        if (!isCancelled()) {
          setLoading(false);
        }
      }
    },
    []
  );

  // Load the lists that depend on the query type
  useEffect(() => {
    let cancelled = false;
    const isCancelled = () => cancelled;
    setListError(undefined);

    switch (query.queryType) {
      case 'alarms':
      case 'objectStatus':
        load(() => datasource.getAlarmObjectList(), setObjectList, setIsLoadingObjects, setListError, isCancelled);
        break;
      case 'summaryTables':
        load(() => datasource.getSummaryTableObjectList(), setObjectList, setIsLoadingObjects, setListError, isCancelled);
        load(() => datasource.getSummaryTableList(), setSummaryTableList, setIsLoadingSummaryTable, setListError, isCancelled);
        break;
      case 'objectQueries':
        load(() => datasource.getObjectQueryObjectList(), setObjectList, setIsLoadingObjects, setListError, isCancelled);
        load(() => datasource.getObjectQueryList(), setObjectQueryList, setIsLoadingObjectQueries, setListError, isCancelled);
        break;
      case 'dciValues':
        load(() => datasource.getDciObjectList(), setObjectList, setIsLoadingObjects, setListError, isCancelled);
        break;
    }

    return () => {
      // A cancelled request never resets its spinner, so reset them here
      cancelled = true;
      setIsLoadingObjects(false);
      setIsLoadingSummaryTable(false);
      setIsLoadingObjectQueries(false);
    };
  }, [query.queryType, datasource, load]);

  // Load the DCI list of the selected root object
  useEffect(() => {
    const objectId = query.sourceObjectId;
    setDciError(undefined);
    if (query.queryType !== 'dciValues' || !objectId) {
      setDciList([]);
      return;
    }
    let cancelled = false;
    load(() => datasource.getDciList(objectId), setDciList, setIsLoadingDcis, setDciError, () => cancelled);
    return () => {
      cancelled = true;
      setIsLoadingDcis(false);
    };
  }, [query.queryType, query.sourceObjectId, datasource, load]);

  // Apply the change and run the query as soon as all required fields of the NEW query are set
  const updateQuery = (next: NetXMSQuery) => {
    onChange(next);
    if (datasource.filterQuery(next)) {
      onRunQuery();
    }
  };

  const onTypeChange = (option: { value: string } | null) => {
    if (!option) {
      return;
    }
    updateQuery({
      ...query,
      queryType: option.value,
      sourceObjectId: undefined,
      dciId: undefined,
      summaryTableId: undefined,
      objectQueryId: undefined,
    });
  };

  return (
    <Stack gap={2} direction="column">
      {(listError || dciError) && (
        <Alert severity="error" title="Failed to load data from NetXMS">
          {listError || dciError}
        </Alert>
      )}

      <InlineField label="Query Type">
        <Combobox
          id="queryType"
          value={query.queryType}
          options={[
            { label: 'Alarms', value: 'alarms' },
            { label: 'Summary Tables', value: 'summaryTables' },
            { label: 'Object Queries', value: 'objectQueries' },
            { label: 'DCI value', value: 'dciValues' },
            { label: 'Object Status', value: 'objectStatus' },
          ]}
          onChange={onTypeChange}
        />
      </InlineField>

      {/* Optional object selector */}
      {(query.queryType === 'alarms' || query.queryType === 'objectQueries') && (
        <InlineField label="Root object" labelWidth={16}>
          <Select
            inputId="sourceObjectId"
            value={query.sourceObjectId}
            isClearable={true}
            onChange={(v) => updateQuery({ ...query, sourceObjectId: v?.value })}
            options={objectList}
            isLoading={isLoadingObjects}
            placeholder="Root object"
            width={32}
          />
        </InlineField>
      )}

      {/* Required object selector */}
      {(query.queryType === 'summaryTables' || query.queryType === 'dciValues' || query.queryType === 'objectStatus') && (
        <InlineField label="Root object" labelWidth={16}>
          <Select
            value={query.sourceObjectId}
            onChange={(v) => updateQuery({ ...query, sourceObjectId: v?.value, dciId: undefined })}
            options={objectList}
            isLoading={isLoadingObjects}
            placeholder="Root object"
            width={32}
          />
        </InlineField>
      )}

      {query.queryType === 'summaryTables' && (
        <InlineField label="Summary table" labelWidth={16}>
          <Select
            value={query.summaryTableId}
            onChange={(v) => updateQuery({ ...query, summaryTableId: v?.value })}
            options={summaryTableList}
            isLoading={isLoadingSummaryTable}
            placeholder="Summary table"
            width={32}
          />
        </InlineField>
      )}

      {query.queryType === 'objectQueries' && (
        <>
          <InlineField label="Object query" labelWidth={16}>
            <Select
              value={query.objectQueryId}
              onChange={(v) => updateQuery({ ...query, objectQueryId: v?.value })}
              options={objectQueryList}
              isLoading={isLoadingObjectQueries}
              placeholder="Select query"
              width={32}
            />
          </InlineField>
          <InlineField
            label="Query parameters"
            labelWidth={16}
            tooltip={'Input field values for the query as a JSON object, e.g. {"name": "value"}'}
          >
            <textarea
              value={query.queryParameters || ''}
              onChange={(e) => updateQuery({ ...query, queryParameters: e.target.value })}
              placeholder='{"name": "value"}'
              style={{ width: '32em', height: '5em' }}
            />
          </InlineField>
        </>
      )}

      {query.queryType === 'dciValues' && (
        <InlineField label="DCI" labelWidth={16}>
          <Select
            value={query.dciId}
            onChange={(v) => updateQuery({ ...query, dciId: v?.value })}
            options={dciList}
            isLoading={isLoadingDcis}
            placeholder="Select DCI"
            width={32}
          />
        </InlineField>
      )}
    </Stack>
  );
}
