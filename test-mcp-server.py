#!/usr/bin/env python3
"""
Simple test MCP server for testing the echo tool and other basic functionality.
Implements the Model Context Protocol (MCP) over stdio.
"""

import json
import sys
import os
from typing import Any, Dict, List, Optional


class MCPServer:
    def __init__(self):
        self.initialized = False
    
    def handle_message(self, message: Dict[str, Any]) -> Optional[Dict[str, Any]]:
        """Handle incoming MCP protocol messages"""
        method = message.get("method")
        
        if method == "initialize":
            return self._handle_initialize(message)
        elif method == "tools/list":
            return self._handle_list_tools(message)
        elif method == "tools/call":
            return self._handle_call_tool(message)
        else:
            return {
                "jsonrpc": "2.0",
                "id": message.get("id"),
                "error": {
                    "code": -32601,
                    "message": f"Unknown method: {method}"
                }
            }
    
    def _handle_initialize(self, message: Dict[str, Any]) -> Dict[str, Any]:
        """Handle the initialize request"""
        self.initialized = True
        return {
            "jsonrpc": "2.0",
            "id": message.get("id"),
            "result": {
                "protocolVersion": "2024-11-05",
                "capabilities": {
                    "tools": {}
                },
                "serverInfo": {
                    "name": "test-mcp-server",
                    "version": "1.0.0"
                }
            }
        }
    
    def _handle_list_tools(self, message: Dict[str, Any]) -> Dict[str, Any]:
        """Handle the tools/list request"""
        if not self.initialized:
            return self._error_response(message.get("id"), "Server not initialized")
        
        tools = [
            {
                "name": "echo",
                "description": "Echo back the provided message",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "message": {
                            "type": "string",
                            "description": "The message to echo back"
                        }
                    },
                    "required": ["message"]
                }
            },
            {
                "name": "get_env",
                "description": "Get an environment variable value",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "name": {
                            "type": "string",
                            "description": "The environment variable name"
                        }
                    },
                    "required": ["name"]
                }
            },
            {
                "name": "list_files",
                "description": "List files in a directory",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "path": {
                            "type": "string",
                            "description": "The directory path to list"
                        }
                    },
                    "required": ["path"]
                }
            }
        ]
        
        return {
            "jsonrpc": "2.0",
            "id": message.get("id"),
            "result": {
                "tools": tools
            }
        }
    
    def _handle_call_tool(self, message: Dict[str, Any]) -> Dict[str, Any]:
        """Handle the tools/call request"""
        if not self.initialized:
            return self._error_response(message.get("id"), "Server not initialized")
        
        params = message.get("params", {})
        tool_name = params.get("name")
        arguments = params.get("arguments", {})
        
        if tool_name == "echo":
            return self._handle_echo(message.get("id"), arguments)
        elif tool_name == "get_env":
            return self._handle_get_env(message.get("id"), arguments)
        elif tool_name == "list_files":
            return self._handle_list_files(message.get("id"), arguments)
        else:
            return self._error_response(message.get("id"), f"Unknown tool: {tool_name}")
    
    def _handle_echo(self, request_id: Any, arguments: Dict[str, Any]) -> Dict[str, Any]:
        """Handle the echo tool call"""
        message = arguments.get("message", "")
        
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "result": {
                "content": [
                    {
                        "type": "text",
                        "text": f"Echo: {message}"
                    }
                ]
            }
        }
    
    def _handle_get_env(self, request_id: Any, arguments: Dict[str, Any]) -> Dict[str, Any]:
        """Handle the get_env tool call"""
        env_name = arguments.get("name", "")
        env_value = os.environ.get(env_name)
        
        if env_value is None:
            text = f"Environment variable '{env_name}' not found"
        else:
            text = f"Environment variable '{env_name}' = '{env_value}'"
        
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "result": {
                "content": [
                    {
                        "type": "text",
                        "text": text
                    }
                ]
            }
        }
    
    def _handle_list_files(self, request_id: Any, arguments: Dict[str, Any]) -> Dict[str, Any]:
        """Handle the list_files tool call"""
        path = arguments.get("path", ".")
        
        try:
            files = os.listdir(path)
            files.sort()
            file_list = "\n".join(files)
            text = f"Files in '{path}':\n{file_list}"
        except OSError as e:
            text = f"Error listing files in '{path}': {e}"
        
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "result": {
                "content": [
                    {
                        "type": "text",
                        "text": text
                    }
                ]
            }
        }
    
    def _error_response(self, request_id: Any, message: str) -> Dict[str, Any]:
        """Create an error response"""
        return {
            "jsonrpc": "2.0",
            "id": request_id,
            "error": {
                "code": -32000,
                "message": message
            }
        }
    
    def run(self):
        """Run the server, reading from stdin and writing to stdout"""
        for line in sys.stdin:
            line = line.strip()
            if not line:
                continue
            
            try:
                message = json.loads(line)
                response = self.handle_message(message)
                if response:
                    print(json.dumps(response), flush=True)
            except json.JSONDecodeError as e:
                # Send error response for malformed JSON
                error_response = {
                    "jsonrpc": "2.0",
                    "id": None,
                    "error": {
                        "code": -32700,
                        "message": f"Parse error: {e}"
                    }
                }
                print(json.dumps(error_response), flush=True)
            except Exception as e:
                # Send generic error response
                error_response = {
                    "jsonrpc": "2.0",
                    "id": None,
                    "error": {
                        "code": -32603,
                        "message": f"Internal error: {e}"
                    }
                }
                print(json.dumps(error_response), flush=True)


if __name__ == "__main__":
    server = MCPServer()
    server.run()
