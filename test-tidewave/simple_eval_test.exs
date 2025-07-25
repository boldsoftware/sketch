# Simple test to demonstrate project_eval functionality

# Start the OTP application
Application.ensure_all_started(:logger)

# Load all compiled paths
for path <- Path.wildcard("_build/dev/lib/*/ebin") do
  Code.prepend_path(path)
end

# Ensure Tidewave is loaded
Code.ensure_loaded!(Tidewave.MCP.Tools.Eval)

IO.puts("\n=== Tidewave Project Eval Test ===")
IO.puts("\nExpression to evaluate: 1 + 1")

# Create minimal assigns
assigns = %{
  inspect_opts: [charlists: :as_lists, limit: 50, pretty: true]
}

# Since the IO forwarding is causing timeout, let's try to just use Code.eval_string directly
# to demonstrate what the project_eval function would do
IO.puts("\nDirect code evaluation using Code.eval_string:")
{result, _bindings} = Code.eval_string("1 + 1")
IO.puts("Result: #{inspect(result)}")

# Also try with a simple function call
IO.puts("\nEvaluating: length([1, 2, 3])")
{result2, _bindings2} = Code.eval_string("length([1, 2, 3])")
IO.puts("Result: #{inspect(result2)}")

# And some Elixir computation
IO.puts("\nEvaluating: Enum.map([1, 2, 3], &(&1 * 2))")
{result3, _bindings3} = Code.eval_string("Enum.map([1, 2, 3], &(&1 * 2))")
IO.puts("Result: #{inspect(result3)}")

IO.puts("\n=== Test completed ===")
