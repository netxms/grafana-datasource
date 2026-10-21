import React, { useState, useEffect, useCallback } from 'react';
import { Alert, InlineField, Stack, Combobox, Select, TextArea } from '@grafana/ui';
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

// An error of one dropdown list, tagged with what it was loaded for (the query type or the
// root object), so it is only shown while the editor still shows that query type / object
type LoadError = { context: string; message: string };

// The DCI list, tagged with the root object it belongs to
type DciList = { objectId?: string; options: Option[] };

export function QueryEditor({ query, onChange, onRunQuery, datasource }: Props) {
  const [objectList, setObjectList] = useState<Option[]>([]);
  const [isLoadingObjects, setIsLoadingObjects] = useState(false);
  const [summaryTableList, setSummaryTableList] = useState<Option[]>([]);
  const [isLoadingSummaryTable, setIsLoadingSummaryTable] = useState(false);
  const [objectQueryList, setObjectQueryList] = useState<Option[]>([]);
  const [isLoadingObjectQueries, setIsLoadingObjectQueries] = useState(false);
  const [dciList, setDciList] = useState<DciList>({ options: [] });
  const [isLoadingDcis, setIsLoadingDcis] = useState(false);
  const [loadErrors, setLoadErrors] = useState<Record<string, LoadError>>({});

  // Loads one dropdown list. Only setLoading(true) runs synchronously from the calling effect;
  // the list, its error and the end of loading are set once the request completes, and are
  // dropped via isCancelled if the request was superseded (query type or root object changed
  // meanwhile).
  const load = useCallback(
    async (
      list: string,
      context: string,
      fetch: () => Promise<ObjectToIdList>,
      setList: (options: Option[]) => void,
      setLoading: (loading: boolean) => void,
      isCancelled: () => boolean
    ) => {
      setLoading(true);
      try {
        const response = await fetch();
        if (!isCancelled()) {
          setList(formatOptions(response));
          setLoadErrors(({ [list]: _cleared, ...rest }) => rest);
        }
      } catch (err) {
        if (!isCancelled()) {
          setList([]);
          setLoadErrors((prev) => ({ ...prev, [list]: { context, message: errorMessage(err) } }));
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
    const queryType = query.queryType;
    if (!queryType) {
      return;
    }
    let cancelled = false;
    const isCancelled = () => cancelled;

    switch (queryType) {
      case 'alarms':
      case 'objectStatus':
        load('objects', queryType, () => datasource.getAlarmObjectList(), setObjectList, setIsLoadingObjects, isCancelled);
        break;
      case 'summaryTables':
        load('objects', queryType, () => datasource.getSummaryTableObjectList(), setObjectList, setIsLoadingObjects, isCancelled);
        load('summaryTables', queryType, () => datasource.getSummaryTableList(), setSummaryTableList, setIsLoadingSummaryTable, isCancelled);
        break;
      case 'objectQueries':
        load('objects', queryType, () => datasource.getObjectQueryObjectList(), setObjectList, setIsLoadingObjects, isCancelled);
        load('objectQueries', queryType, () => datasource.getObjectQueryList(), setObjectQueryList, setIsLoadingObjectQueries, isCancelled);
        break;
      case 'dciValues':
        load('objects', queryType, () => datasource.getDciObjectList(), setObjectList, setIsLoadingObjects, isCancelled);
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
    if (query.queryType !== 'dciValues' || !objectId) {
      return;
    }
    let cancelled = false;
    load(
      'dcis',
      objectId,
      () => datasource.getDciList(objectId),
      (options) => setDciList({ objectId, options }),
      setIsLoadingDcis,
      () => cancelled
    );
    return () => {
      cancelled = true;
      setIsLoadingDcis(false);
    };
  }, [query.queryType, query.sourceObjectId, datasource, load]);

  // Only show what belongs to the query type / root object currently shown in the editor
  const dciOptions = dciList.objectId === query.sourceObjectId ? dciList.options : [];
  const loadError = Object.entries(loadErrors).find(([list, e]) =>
    list === 'dcis'
      ? query.queryType === 'dciValues' && e.context === query.sourceObjectId
      : e.context === query.queryType
  )?.[1].message;

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
      {loadError && (
        <Alert severity="error" title="Failed to load data from NetXMS">
          {loadError}
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
            inputId="sourceObjectId"
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
            inputId="summaryTableId"
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
              inputId="objectQueryId"
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
            grow
          >
            {/* Runs on blur rather than per keystroke: each run is a server-side NXSL query */}
            <TextArea
              id="queryParameters"
              value={query.queryParameters || ''}
              onChange={(e) => onChange({ ...query, queryParameters: e.currentTarget.value })}
              onBlur={() => {
                if (datasource.filterQuery(query)) {
                  onRunQuery();
                }
              }}
              placeholder='{"name": "value"}'
              rows={4}
            />
          </InlineField>
        </>
      )}

      {query.queryType === 'dciValues' && (
        <InlineField label="DCI" labelWidth={16}>
          <Select
            value={query.dciId}
            onChange={(v) => updateQuery({ ...query, dciId: v?.value })}
            options={dciOptions}
            isLoading={isLoadingDcis}
            placeholder="Select DCI"
            width={32}
          />
        </InlineField>
      )}
    </Stack>
  );
}
