# dear_llm.md

If you find yourself needing to repeatedly give the LLM instructions on how to work with your
repository, you can create a file named `dear_llm.md` in the root of your repository and put the
instructions there. Sketch will read this file at the beginning of a session and use it to inform
the LLM how to work with your repository.

Keep the file short and to the point, as it will be read every time a new session starts and you
do not want to distract the LLM with too much information.

The best way to decide what should go in your `dear_llm.md` file is to read the transcript of how
Sketch works with your code. If you find on repeated sessions that the model has to make multiple
attempts to do a standard task, like starting a server as a background process, or what endpoint
to visit to see a particular part of your application, a short note can be helpful.

It is also common to add commit message style guidance. (You can ask Sketch to analyze your
existing commit messages and add the guidelines for you!)
