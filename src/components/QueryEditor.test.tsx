import React, { act, useState } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { DataSource } from '../datasource';
import { NetXMSQuery } from '../types';
import { QueryEditor } from './QueryEditor';

// The real filterQuery, with only the list requests the object query editor makes stubbed out
function stubDatasource(): DataSource {
  const ds = Object.create(DataSource.prototype) as DataSource;
  ds.getObjectQueryObjectList = jest.fn().mockResolvedValue({ objects: [] });
  ds.getObjectQueryList = jest.fn().mockResolvedValue({ objects: [] });
  return ds;
}

// Feeds every change back in as the query prop, the way Grafana does
function ControlledEditor({
  initial,
  datasource,
  onChange,
  onRunQuery,
}: {
  initial: NetXMSQuery;
  datasource: DataSource;
  onChange: (query: NetXMSQuery) => void;
  onRunQuery: () => void;
}) {
  const [query, setQuery] = useState(initial);
  return (
    <QueryEditor
      query={query}
      datasource={datasource}
      onChange={(next) => {
        onChange(next);
        setQuery(next);
      }}
      onRunQuery={onRunQuery}
    />
  );
}

async function renderEditor(initial: NetXMSQuery) {
  const onChange = jest.fn();
  const onRunQuery = jest.fn();
  await act(async () => {
    render(
      <ControlledEditor initial={initial} datasource={stubDatasource()} onChange={onChange} onRunQuery={onRunQuery} />
    );
  });
  return { onChange, onRunQuery, parameters: screen.getByPlaceholderText('{"name": "value"}') };
}

const objectQuery: NetXMSQuery = { refId: 'A', queryType: 'objectQueries', objectQueryId: '1', queryParameters: '' };

describe('QueryEditor query parameters', () => {
  it('runs the query on blur, not while typing', async () => {
    const { onChange, onRunQuery, parameters } = await renderEditor(objectQuery);
    const values = ['{"name":"a"}', '{"name":"ab"}', '{"name":"abc"}'];

    for (const value of values) {
      fireEvent.change(parameters, { target: { value } });
    }
    expect(onChange.mock.calls.map(([query]) => query.queryParameters)).toEqual(values);
    expect(onRunQuery).not.toHaveBeenCalled();

    fireEvent.blur(parameters);
    expect(onRunQuery).toHaveBeenCalledTimes(1);
  });

  it.each([
    { name: 'malformed JSON', query: { ...objectQuery, queryParameters: '{"name":' }, runs: 0 },
    { name: 'empty parameters with a query selected', query: objectQuery, runs: 1 },
    { name: 'no query selected', query: { ...objectQuery, objectQueryId: undefined, queryParameters: '{}' }, runs: 0 },
  ])('on blur with $name runs the query $runs time(s)', async ({ query, runs }) => {
    const { onRunQuery, parameters } = await renderEditor(query);
    fireEvent.blur(parameters);
    expect(onRunQuery).toHaveBeenCalledTimes(runs);
  });
});
