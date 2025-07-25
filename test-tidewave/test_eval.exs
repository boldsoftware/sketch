# Test script to use Tidewave project_eval function

# Load all compiled paths
for path <- Path.wildcard("_build/dev/lib/*/ebin") do
  Code.prepend_path(path)
end

# Load the module
Code.ensure_loaded!(Tidewave.MCP.Tools.Eval)

# Create minimal assigns structure
assigns = %{
  inspect_opts: [charlists: :as_lists, limit: 50, pretty: true]
}

# Test the project_eval function with "1 + 1"
IO.puts("Testing tidewave project_eval with '1 + 1':")
result = Tidewave.MCP.Tools.Eval.project_eval(%{"code" => "1 + 1"}, assigns)
IO.inspect(result, label: "project_eval result")


# Also test with a more complex expression
IO.puts("\nTesting with a more complex expression:")
result2 = Tidewave.MCP.Tools.Eval.project_eval(%{"code" => "IO.puts(\"Hello from eval!\"); 2 * 3"}, assigns)
IO.inspect(result2, label: "complex expression result")
