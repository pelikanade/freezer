{ pkgs, ... }:

{
  languages.go.enable = true;
  packages = [ pkgs.git pkgs.git-lfs pkgs.minio-client ];

  services.minio = {
    enable = true;
    package = pkgs.silo;
    listenAddress = "127.0.0.1:19000";
    consoleAddress = "127.0.0.1:19001";
    region = "auto";
    buckets = [ "freezer-test" ];
  };

  env = {
    CGO_ENABLED = "0";
    AWS_ACCESS_KEY_ID = "minioadmin";
    AWS_SECRET_ACCESS_KEY = "minioadmin";
    AWS_EC2_METADATA_DISABLED = "true";
    FREEZER_TEST_ENDPOINT = "http://127.0.0.1:19000";
    FREEZER_TEST_BUCKET = "freezer-test";
    FREEZER_TEST_BUSYBOX = "${pkgs.pkgsStatic.busybox}/bin/busybox";
  };

  enterTest = ''
    go test -v -count=1 -timeout=15m ./... | tee .devenv/acceptance.log
  '';
}
