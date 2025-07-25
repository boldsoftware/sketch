#!/usr/bin/env elixir

# Tidewave Project Eval Demo
# This script demonstrates the tidewave_project_eval functionality
# by evaluating "1 + 1" using the same core logic as the MCP tool

# Start required applications
Application.ensure_all_started(:logger)

# Load all compiled paths
for path <- Path.wildcard("_build/dev/lib/*/ebin") do
  Code.prepend_path(path)
end

defmodule TidewaveProjectEvalDemo do
  @moduledoc """
  Demo module that replicates the tidewave_project_eval functionality
  """

  def eval_code(code, inspect_opts \\ []) do
    inspect_opts = 
      inspect_opts
      |> Keyword.put_new(:charlists, :as_lists)
      |> Keyword.put_new(:limit, 50)
      |> Keyword.put_new(:pretty, true)
    
    try do
      # This is the core logic from Tidewave.MCP.Tools.Eval.eval_with_captured_io/2
      {result, _bindings} = Code.eval_string(code, [], env())
      {:ok, inspect(result, inspect_opts)}
    catch
      kind, reason -> 
        {:error, Exception.format(kind, reason, __STACKTRACE__)}
    end
  end
  
  # Import IEx helpers like the original implementation
  defp env do
    import IEx.Helpers, warn: false
    __ENV__
  end
  
  def project_eval(args) do
    case args do
      %{"code" => code} -> eval_code(code)
      %{code: code} -> eval_code(code)
      _ -> {:error, :invalid_arguments}
    end
  end
end

IO.puts("\n🌊 Tidewave Project Eval Demo 🌊")
IO.puts("=" <> String.duplicate("=", 35))

# Test 1: Basic arithmetic
IO.puts("\n📊 Test 1: Evaluating '1 + 1'")
result1 = TidewaveProjectEvalDemo.project_eval(%{"code" => "1 + 1"})
IO.puts("Input: 1 + 1")
case result1 do
  {:ok, result} -> IO.puts("Result: #{result}")
  {:error, error} -> IO.puts("Error: #{error}")
end

# Test 2: String manipulation
IO.puts("\n📝 Test 2: String operations")
result2 = TidewaveProjectEvalDemo.project_eval(%{"code" => "\"Hello, \" <> \"World!\""})
IO.puts("Input: \"Hello, \" <> \"World!\"") 
case result2 do
  {:ok, result} -> IO.puts("Result: #{result}")
  {:error, error} -> IO.puts("Error: #{error}")
end

# Test 3: List operations
IO.puts("\n📋 Test 3: List operations")
result3 = TidewaveProjectEvalDemo.project_eval(%{"code" => "Enum.sum([1, 2, 3, 4, 5])"})
IO.puts("Input: Enum.sum([1, 2, 3, 4, 5])")
case result3 do
  {:ok, result} -> IO.puts("Result: #{result}")
  {:error, error} -> IO.puts("Error: #{error}")
end

# Test 4: Pattern matching
IO.puts("\n🎯 Test 4: Pattern matching")
result4 = TidewaveProjectEvalDemo.project_eval(%{"code" => "{:ok, value} = {:ok, 42}; value"})
IO.puts("Input: {:ok, value} = {:ok, 42}; value")
case result4 do
  {:ok, result} -> IO.puts("Result: #{result}")
  {:error, error} -> IO.puts("Error: #{error}")
end

# Test 5: Function definition and call
IO.puts("\n🔧 Test 5: Function definition")
result5 = TidewaveProjectEvalDemo.project_eval(%{"code" => "square = fn x -> x * x end; square.(5)"})
IO.puts("Input: square = fn x -> x * x end; square.(5)")
case result5 do
  {:ok, result} -> IO.puts("Result: #{result}")
  {:error, error} -> IO.puts("Error: #{error}")
end

IO.puts("\n✅ All evaluations completed!")
IO.puts("\nThis demonstrates the core functionality of tidewave_project_eval.")
IO.puts("The function can evaluate arbitrary Elixir expressions in the project context.")
