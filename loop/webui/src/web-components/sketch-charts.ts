import "./vega-embed";
import { css, html, LitElement, PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { TopLevelSpec } from "vega-lite";
import type { TimelineMessage } from "../types";
import "vega-embed";
import { VisualizationSpec } from "vega-embed";

/**
 * Web component for rendering charts related to the timeline data
 * Displays cumulative cost over time and message timing visualization
 */
@customElement("sketch-charts")
export class SketchCharts extends LitElement {
  @property({ type: Array })
  messages: TimelineMessage[] = [];

  @state()
  private chartData: { timestamp: Date; cost: number }[] = [];

  // We need to make the styles available to Vega-Embed when it's rendered
  static styles = css`
    :host {
      display: block;
      width: 100%;
    }

    .chart-container {
      padding: 20px;
      background-color: #fff;
      border-radius: 8px;
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
      margin-bottom: 20px;
    }

    .chart-section {
      margin-bottom: 30px;
    }

    .chart-section h3 {
      margin-top: 0;
      margin-bottom: 15px;
      font-size: 18px;
      color: #333;
      border-bottom: 1px solid #eee;
      padding-bottom: 8px;
    }

    .chart-content {
      width: 100%;
      min-height: 300px;
    }

    .loader {
      border: 4px solid #f3f3f3;
      border-radius: 50%;
      border-top: 4px solid #3498db;
      width: 40px;
      height: 40px;
      margin: 20px auto;
      animation: spin 2s linear infinite;
    }

    @keyframes spin {
      0% {
        transform: rotate(0deg);
      }
      100% {
        transform: rotate(360deg);
      }
    }
  `;

  constructor() {
    super();
    this.chartData = [];
  }

  private calculateCumulativeCostData(
    messages: TimelineMessage[]
  ): { timestamp: Date; cost: number }[] {
    if (!messages || messages.length === 0) {
      return [];
    }

    let cumulativeCost = 0;
    const data: { timestamp: Date; cost: number }[] = [];

    for (const message of messages) {
      if (message.timestamp && message.usage && message.usage.cost_usd) {
        const timestamp = new Date(message.timestamp);
        cumulativeCost += message.usage.cost_usd;

        data.push({
          timestamp,
          cost: cumulativeCost,
        });
      }
    }

    return data;
  }

  protected willUpdate(changedProperties: PropertyValues): void {
    if (changedProperties.has("messages")) {
      this.chartData = this.calculateCumulativeCostData(this.messages);
    }
  }

  private getMessagesChartSpec(): VisualizationSpec {
    try {
      const allMessages = this.messages;
      if (!Array.isArray(allMessages) || allMessages.length === 0) {
        return null;
      }

      // Sort messages chronologically
      allMessages.sort((a, b) => {
        const dateA = a.timestamp ? new Date(a.timestamp).getTime() : 0;
        const dateB = b.timestamp ? new Date(b.timestamp).getTime() : 0;
        return dateA - dateB;
      });

      // Create unique indexes for all messages
      const messageIndexMap = new Map<string, number>();
      let messageIdx = 0;
      
      // First pass: Process parent messages
      allMessages.forEach((msg, index) => {
        // Create a unique ID for each message to track its position
        const msgId = msg.timestamp ? msg.timestamp.toString() : `msg-${index}`;
        messageIndexMap.set(msgId, messageIdx++);
      });
      
      // Process tool calls from messages to account for filtered out tool messages
      const toolCallData: any[] = [];
      allMessages.forEach((msg) => {
        if (msg.tool_calls && msg.tool_calls.length > 0) {
          msg.tool_calls.forEach((toolCall) => {
            if (toolCall.result_message) {
              // Add this tool result message to our data
              const resultMsg = toolCall.result_message;
              
              // Important: use the original message's idx to maintain the correct order
              // The original message idx value is what we want to show in the chart
              if (resultMsg.idx !== undefined) {
                // If the tool call has start/end times, add it to bar data, otherwise to point data
                if (resultMsg.start_time && resultMsg.end_time) {
                  toolCallData.push({
                    type: 'bar',
                    index: resultMsg.idx,  // Use actual idx from message
                    message_type: 'tool',
                    content: resultMsg.content || '',
                    tool_name: resultMsg.tool_name || toolCall.name || '',
                    tool_input: toolCall.input || '',
                    tool_result: resultMsg.tool_result || '',
                    start_time: new Date(resultMsg.start_time).toISOString(),
                    end_time: new Date(resultMsg.end_time).toISOString(),
                    message: JSON.stringify(resultMsg, null, 2)
                  });
                } else if (resultMsg.timestamp) {
                  toolCallData.push({
                    type: 'point',
                    index: resultMsg.idx,  // Use actual idx from message
                    message_type: 'tool',
                    content: resultMsg.content || '',
                    tool_name: resultMsg.tool_name || toolCall.name || '',
                    tool_input: toolCall.input || '',
                    tool_result: resultMsg.tool_result || '',
                    time: new Date(resultMsg.timestamp).toISOString(),
                    message: JSON.stringify(resultMsg, null, 2)
                  });
                }
              }
            }
          });
        }
      });

      // Prepare data for messages with start_time and end_time (bar marks)
      const barData = allMessages
        .filter((msg) => msg.start_time && msg.end_time) // Only include messages with explicit start and end times
        .map((msg) => {
          // Parse start and end times
          const startTime = new Date(msg.start_time!);
          const endTime = new Date(msg.end_time!);

          // Use the message idx directly for consistent ordering
          const index = msg.idx;

          // Truncate content for tooltip readability
          const displayContent = msg.content
            ? msg.content.length > 100
              ? msg.content.substring(0, 100) + "..."
              : msg.content
            : "No content";

          // Prepare tool input and output for tooltip if applicable
          const toolInput = msg.input
            ? msg.input.length > 100
              ? msg.input.substring(0, 100) + "..."
              : msg.input
            : "";

          const toolResult = msg.tool_result
            ? msg.tool_result.length > 100
              ? msg.tool_result.substring(0, 100) + "..."
              : msg.tool_result
            : "";

          return {
            index: index,
            message_type: msg.type,
            content: displayContent,
            tool_name: msg.tool_name || "",
            tool_input: toolInput,
            tool_result: toolResult,
            start_time: startTime.toISOString(),
            end_time: endTime.toISOString(),
            message: JSON.stringify(msg, null, 2), // Full message for detailed inspection
          };
        });

      // Prepare data for messages with timestamps only (point marks)
      const pointData = allMessages
        .filter((msg) => msg.timestamp && !(msg.start_time && msg.end_time)) // Only messages with timestamp but without start/end times
        .map((msg) => {
          // Get the timestamp
          const timestamp = new Date(msg.timestamp!);

          // Use the message idx directly for consistent ordering
          const index = msg.idx;

          // Truncate content for tooltip readability
          const displayContent = msg.content
            ? msg.content.length > 100
              ? msg.content.substring(0, 100) + "..."
              : msg.content
            : "No content";

          // Prepare tool input and output for tooltip if applicable
          const toolInput = msg.input
            ? msg.input.length > 100
              ? msg.input.substring(0, 100) + "..."
              : msg.input
            : "";

          const toolResult = msg.tool_result
            ? msg.tool_result.length > 100
              ? msg.tool_result.substring(0, 100) + "..."
              : msg.tool_result
            : "";

          return {
            index: index,
            message_type: msg.type,
            content: displayContent,
            tool_name: msg.tool_name || "",
            tool_input: toolInput,
            tool_result: toolResult,
            time: timestamp.toISOString(),
            message: JSON.stringify(msg, null, 2), // Full message for detailed inspection
          };
        });
        
      // Add tool call data to the appropriate arrays
      const toolBarData = toolCallData.filter(d => d.type === 'bar').map(d => {
        delete d.type;
        return d;
      });
      
      const toolPointData = toolCallData.filter(d => d.type === 'point').map(d => {
        delete d.type;
        return d;
      });

      // Check if we have any data to display
      if (barData.length === 0 && pointData.length === 0 && 
          toolBarData.length === 0 && toolPointData.length === 0) {
        return null;
      }

      // Calculate height based on number of unique messages
      const chartHeight = 20 * Math.min(allMessages.length, 25); // Max 25 visible at once

      // Create a layered Vega-Lite spec combining bars and points
      const messagesSpec: TopLevelSpec = {
        $schema: "https://vega.github.io/schema/vega-lite/v5.json",
        description: "Message Timeline",
        width: "container",
        height: chartHeight,
        layer: [],
      };

      // Add bar layer if we have bar data
      if (barData.length > 0 || toolBarData.length > 0) {
        const combinedBarData = [...barData, ...toolBarData];
        messagesSpec.layer.push({
          data: { values: combinedBarData },
          mark: {
            type: "bar",
            height: 16,
          },
          encoding: {
            x: {
              field: "start_time",
              type: "temporal",
              title: "Time",
              axis: {
                format: "%H:%M:%S",
                title: "Time",
                labelAngle: -45,
              },
            },
            x2: { field: "end_time" },
            y: {
              field: "index",
              type: "ordinal",
              title: "Message Index",
              axis: {
                grid: true,
              },
            },
            color: {
              field: "message_type",
              type: "nominal",
              title: "Message Type",
              legend: {},
            },
            tooltip: [
              { field: "message_type", type: "nominal", title: "Type" },
              { field: "tool_name", type: "nominal", title: "Tool" },
              {
                field: "start_time",
                type: "temporal",
                title: "Start Time",
                format: "%H:%M:%S.%L",
              },
              {
                field: "end_time",
                type: "temporal",
                title: "End Time",
                format: "%H:%M:%S.%L",
              },
              { field: "content", type: "nominal", title: "Content" },
              { field: "tool_input", type: "nominal", title: "Tool Input" },
              { field: "tool_result", type: "nominal", title: "Tool Result" },
            ],
          },
        });
      }

      // Add point layer if we have point data
      if (pointData.length > 0 || toolPointData.length > 0) {
        const combinedPointData = [...pointData, ...toolPointData];
        messagesSpec.layer.push({
          data: { values: combinedPointData },
          mark: {
            type: "point",
            size: 100,
            filled: true,
          },
          encoding: {
            x: {
              field: "time",
              type: "temporal",
              title: "Time",
              axis: {
                format: "%H:%M:%S",
                title: "Time",
                labelAngle: -45,
              },
            },
            y: {
              field: "index",
              type: "ordinal",
              title: "Message Index",
            },
            color: {
              field: "message_type",
              type: "nominal",
              title: "Message Type",
            },
            tooltip: [
              { field: "message_type", type: "nominal", title: "Type" },
              { field: "tool_name", type: "nominal", title: "Tool" },
              {
                field: "time",
                type: "temporal",
                title: "Timestamp",
                format: "%H:%M:%S.%L",
              },
              { field: "content", type: "nominal", title: "Content" },
              { field: "tool_input", type: "nominal", title: "Tool Input" },
              { field: "tool_result", type: "nominal", title: "Tool Result" },
            ],
          },
        });
      }
      return messagesSpec;
    } catch (error) {
      console.error("Error rendering messages chart:", error);
    }
  }

  render() {
    const costSpec = this.createCostChartSpec();
    const messagesSpec = this.getMessagesChartSpec();

    return html`
      <div class="chart-container" id="chartContainer">
        <div class="chart-section">
          <h3>Dollar Usage Over Time</h3>
          <div class="chart-content">
          ${this.chartData.length > 0 ? 
            html`<vega-embed .spec=${costSpec}></vega-embed>` 
            : html`<p>No cost data available to display.</p>`}
          </div>
        </div>
        <div class="chart-section">
          <h3>Message Timeline</h3>
          <div class="chart-content">
          ${messagesSpec?.data ? 
              html`<vega-embed .spec=${messagesSpec}></vega-embed>`
              : html`<p>No messages available to display.</p>`}
          </div>
        </div>
      </div>
    `;
  }

  private createCostChartSpec(): VisualizationSpec {
    return {
      $schema: "https://vega.github.io/schema/vega-lite/v5.json",
      description: "Cumulative cost over time",
      width: "container",
      height: 300,
      data: {
        values: this.chartData.map((d) => ({
          timestamp: d.timestamp.toISOString(),
          cost: d.cost,
        })),
      },
      mark: {
        type: "line",
        point: true,
      },
      encoding: {
        x: {
          field: "timestamp",
          type: "temporal",
          title: "Time",
          axis: {
            format: "%H:%M:%S",
            title: "Time",
            labelAngle: -45,
          },
        },
        y: {
          field: "cost",
          type: "quantitative",
          title: "Cumulative Cost (USD)",
          axis: {
            format: "$,.4f",
          },
        },
        tooltip: [
          {
            field: "timestamp",
            type: "temporal",
            title: "Time",
            format: "%Y-%m-%d %H:%M:%S",
          },
          {
            field: "cost",
            type: "quantitative",
            title: "Cumulative Cost",
            format: "$,.4f",
          },
        ],
      },
    };
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-charts": SketchCharts;
  }
}
