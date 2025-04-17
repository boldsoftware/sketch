# Stand-alone demo pages for sketch web components

These are handy for iterating on specific component UI issues in isolation from the rest of the sketch application, and without having to start a full backend to serve the full frontend app UI.

# How to use this demo directory to iterate on component development

From the `loop/webui` directory:

1. In one shell, run `npm run watch` to build the web components and watch for changes
1. In another shell, run `npm run demo` to start a local web server to serve the demo pages
1. open http://localhost:8000/src/web-components/demo/ in your browser
1. make edits to the .ts code or to the demo.html files and see how it affects the demo pages in real time

Alternately, use the `webui: watch demo` task in VSCode, which runs all of the above for you.
