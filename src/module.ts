import { DataSourcePlugin } from '@grafana/data';

import { ConfigEditor } from './ConfigEditor';
import { DataSource } from './datasource';
import { FlintAiJsonData, FlintAiQuery, FlintAiSecureJsonData } from './types';

export const plugin = new DataSourcePlugin<DataSource, FlintAiQuery, FlintAiJsonData, FlintAiSecureJsonData>(
  DataSource
).setConfigEditor(ConfigEditor);
