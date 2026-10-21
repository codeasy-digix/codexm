const { defineConfig } = require('@meteorjs/rspack');

// Keep the development asset server local too. Meteor's host:port CLI form
// must not be coerced to a number by the upstream default port calculation.
module.exports = defineConfig(Meteor => Meteor.isClient && Meteor.isDevelopment ? {
  devServer: {
    host: '127.0.0.1',
    port: Number(new URL(process.env.ROOT_URL || 'http://127.0.0.1:4317').port) + 3,
    allowedHosts: ['127.0.0.1', 'localhost'],
  },
} : {});
