import { hmrPlugin, presets } from "@open-wc/dev-server-hmr";

export default {
  port: 8000,
  nodeResolve: true,

  plugins: [
    hmrPlugin({
      include: ["../**/*"],
      presets: [presets.lit],
    }),
  ],
};
