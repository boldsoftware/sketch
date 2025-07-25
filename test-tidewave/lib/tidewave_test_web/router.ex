defmodule TidewaveTestWeb.Router do
  use TidewaveTestWeb, :router

  pipeline :api do
    plug :accepts, ["json"]
  end

  scope "/api", TidewaveTestWeb do
    pipe_through :api
  end
end
