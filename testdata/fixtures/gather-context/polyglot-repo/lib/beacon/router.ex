defmodule Beacon.Router do
  alias BeaconWeb.HealthController

  def routes do
    [{:get, "/health", &HealthController.show/1}]
  end
end
