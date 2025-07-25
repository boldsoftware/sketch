defmodule Tidewave.MCP.Utils do
  @moduledoc false

  @max_line_length 2000

  def truncate_lines(string) when is_binary(string) do
    string
    |> String.split("\n")
    |> Enum.map(&truncate_line/1)
    |> Enum.join("\n")
  end

  defp truncate_line(line) when is_binary(line) do
    length = String.length(line)

    if length > @max_line_length do
      "#{String.slice(line, 0, @max_line_length)} [#{length - @max_line_length} characters truncated] ..."
    else
      line
    end
  end

  def detect_file_line_endings(path) do
    case File.read(path) do
      {:ok, content} ->
        detect_line_endings(content)

      _ ->
        nil
    end
  end

  def detect_line_endings(content) do
    {lf_count, crlf_count} = detect_line_endings(content, 0, 0)

    if crlf_count > lf_count do
      :crlf
    else
      :lf
    end
  end

  defp detect_line_endings(<<"\r\n", rest::binary>>, lf_count, crlf_count) do
    detect_line_endings(rest, lf_count, crlf_count + 1)
  end

  defp detect_line_endings(<<"\n", rest::binary>>, lf_count, crlf_count) do
    detect_line_endings(rest, lf_count + 1, crlf_count)
  end

  defp detect_line_endings(<<_char::utf8, rest::binary>>, lf_count, crlf_count) do
    detect_line_endings(rest, lf_count, crlf_count)
  end

  defp detect_line_endings(<<>>, lf_count, crlf_count) do
    {lf_count, crlf_count}
  end
end
