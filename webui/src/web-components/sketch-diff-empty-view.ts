import { html } from "lit";
import { customElement } from "lit/decorators.js";
import { SketchTailwindElement } from "./sketch-tailwind-element.js";

/**
 * A component that displays helpful information when the diff view is empty
 */
@customElement("sketch-diff-empty-view")
export class SketchDiffEmptyView extends SketchTailwindElement {
  render() {
    return html`
      <div
        class="flex flex-col items-center justify-center h-full w-full box-border"
      >
        <div
          class="m-8 mx-auto max-w-4xl w-11/12 p-8 border-2 border-gray-300 dark:border-neutral-600 rounded-lg shadow-sm bg-white dark:bg-neutral-800 text-center"
        >
          <h2
            class="text-2xl font-semibold mb-6 text-center text-gray-800 dark:text-neutral-200"
          >
            How to use the Diff View
          </h2>

          <p
            class="text-gray-600 dark:text-neutral-400 leading-relaxed text-base text-left mb-4"
          >
            By default, the diff view shows differences between when you started
            Sketch (the "sketch-base" tag) and the current state. Choose a
            commit to look at, or, a range of commits, and navigate across
            files.
          </p>

          <p
            class="text-gray-600 dark:text-neutral-400 leading-relaxed text-base text-left mb-4"
          >
            You can select text to leave comments on the code. These will be
            added to your chat window, and you can click Send to send them along
            to the agent, which will respond in the chat window.
          </p>

          <p
            class="text-gray-600 dark:text-neutral-400 leading-relaxed text-base text-left mb-4"
          >
            If the range includes
            <strong class="font-semibold text-gray-800 dark:text-neutral-200"
              >Uncommitted Changes</strong
            >, you can
            <strong class="font-semibold text-gray-800 dark:text-neutral-200"
              >edit</strong
            >
            the text as well, and it auto-saves. If you want to clear up a
            comment or write your own text, just go ahead and do it! Once you're
            done, though, be sure to commit your changes, either by asking the
            agent to do so or in the Terminal view.
          </p>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-diff-empty-view": SketchDiffEmptyView;
  }
}
