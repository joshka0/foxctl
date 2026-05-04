defmodule BeaconWeb.HealthController do
  alias Beacon.Health.Checker

  def show(conn) do
    Checker.status(conn)
  end
end
