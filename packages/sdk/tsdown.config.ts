import { defineConfig, type UserConfig } from 'tsdown';

const config: UserConfig = defineConfig({
  entry: ['src/index.ts'],
  format: 'esm',
  platform: 'neutral',
  target: 'es2023',
  dts: true,
  // The generated API types live in a private workspace package, so they are
  // inlined into the published declarations.
  deps: { alwaysBundle: ['@bridge/api-types'] },
  clean: true,
});

export default config;
