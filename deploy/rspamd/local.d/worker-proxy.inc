bind_socket = "0.0.0.0:11332";
milter = yes;
timeout = 10s;

upstream "local" {
  default = yes;
  self_scan = yes;
}
