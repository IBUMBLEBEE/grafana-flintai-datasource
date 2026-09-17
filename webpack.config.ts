import type { Configuration } from 'webpack';

import grafanaConfig, { type Env } from './.config/webpack/webpack.config';

const config = async (env: Env): Promise<Configuration> => {
  return grafanaConfig(env);
};

export default config;
