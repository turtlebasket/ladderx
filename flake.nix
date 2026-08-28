{
  description = "Ladder — reproducible build, checks, and development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        inherit (pkgs) lib;

        version = self.shortRev or self.dirtyShortRev or "dev";
        src = lib.cleanSourceWith {
          src = ./.;
          filter =
            path: type:
            lib.cleanSourceFilter path type
            && !(lib.hasSuffix "/cmd/styles.css" (toString path))
            && !(lib.hasSuffix "/styles/output.css" (toString path))
            && !(lib.elem (baseNameOf path) [
              "node_modules"
              "result"
              "tmp"
            ]);
        };

        buildAssets = ''
          asset_tmp="$(mktemp -d)"
          trap 'rm -rf "$asset_tmp"' EXIT
          tailwindcss \
            --input ./styles/input.css \
            --output "$asset_tmp/output.css" \
            --build
          minify \
            --output ./cmd/styles.css \
            "$asset_tmp/output.css"
        '';

        ladder = pkgs.buildGoModule {
          pname = "ladder";
          inherit version src;

          vendorHash = "sha256-mEyW6ZRyxvfu/c//mKYdet0oQchjydi996QXtdO7V0A=";
          nativeBuildInputs = with pkgs; [
            minify
            tailwindcss
          ];
          env.CGO_ENABLED = "0";
          preBuild = buildAssets;
          subPackages = [ "cmd" ];
          ldflags = [
            "-s"
            "-w"
            "-X ladder/handlers.version=${version}"
          ];
          doCheck = false;
          postInstall = ''
            built_binary="$(find "$out/bin" -type f -name cmd -print -quit)"
            mv "$built_binary" "$out/bin/ladder"
            find "$out/bin" -mindepth 1 -type d -empty -delete
          '';

          meta = {
            description = "HTTP web proxy for testing content delivery behavior";
            homepage = "https://github.com/everywall/ladder";
            license = lib.licenses.mit;
            mainProgram = "ladder";
          };
        };

        linux-amd64 = ladder.overrideAttrs (old: {
          pname = "ladder-linux-amd64";
          dontPatchELF = true;
          dontStrip = true;
          preBuild = old.preBuild + ''
            export GOOS=linux
            export GOARCH=amd64
          '';
        });

        assets = pkgs.writeShellApplication {
          name = "ladder-assets";
          runtimeInputs = with pkgs; [
            minify
            tailwindcss
          ];
          text = buildAssets;
        };

        dev = pkgs.writeShellApplication {
          name = "ladder-dev";
          runtimeInputs = [
            assets
            pkgs.air
            pkgs.go
          ];
          text = ''
            ladder-assets
            exec air "$@"
          '';
        };

        test = pkgs.writeShellApplication {
          name = "ladder-test";
          runtimeInputs = [
            assets
            pkgs.go
          ];
          text = ''
            ladder-assets
            go test ./...
          '';
        };

        integration = pkgs.writeShellApplication {
          name = "ladder-integration";
          runtimeInputs = [
            assets
            pkgs.go
          ];
          text = ''
            ladder-assets
            LIVE_TEST_URL="''${LIVE_TEST_URL:-https://www.google.com/robots.txt}" \
              go test -tags=integration -count=1 ./...
          '';
        };

        vet = pkgs.writeShellApplication {
          name = "ladder-vet";
          runtimeInputs = [
            assets
            pkgs.go
          ];
          text = ''
            ladder-assets
            go vet ./...
          '';
        };

        fmt-check = pkgs.writeShellApplication {
          name = "ladder-fmt-check";
          runtimeInputs = with pkgs; [
            gofumpt
            nixfmt
          ];
          text = ''
            unformatted="$(gofumpt -l .)"
            if [[ -n "$unformatted" ]]; then
              echo "Go files need formatting:" >&2
              echo "$unformatted" >&2
              exit 1
            fi
            nixfmt --check flake.nix
          '';
        };

        fmt = pkgs.writeShellApplication {
          name = "ladder-fmt";
          runtimeInputs = with pkgs; [
            gofumpt
            nixfmt
          ];
          text = ''
            gofumpt -w .
            nixfmt flake.nix
          '';
        };

        lint = pkgs.writeShellApplication {
          name = "ladder-lint";
          runtimeInputs = [
            assets
            fmt-check
            pkgs.go
            pkgs.golangci-lint
          ];
          text = ''
            ladder-assets
            ladder-fmt-check
            golangci-lint run -c .golangci-lint.yaml
          '';
        };

        lint-fix = pkgs.writeShellApplication {
          name = "ladder-lint-fix";
          runtimeInputs = [
            assets
            fmt
            pkgs.go
            pkgs.golangci-lint
          ];
          text = ''
            ladder-assets
            ladder-fmt
            golangci-lint run -c .golangci-lint.yaml --fix
          '';
        };

        tidy = pkgs.writeShellApplication {
          name = "ladder-tidy";
          runtimeInputs = [ pkgs.go ];
          text = ''
            go mod tidy
          '';
        };

        prepareCheckSource = ''
          cp -R ${src}/. source
          chmod -R u+w source
          cd source
          cp -R ${ladder.goModules} vendor
          ${buildAssets}
          export GOCACHE="$TMPDIR/go-cache"
          export GOLANGCI_LINT_CACHE="$TMPDIR/golangci-lint-cache"
          export GOFLAGS="-mod=vendor"
          export GOPROXY=off
          export GOSUMDB=off
          export GOTOOLCHAIN=local
        '';

        mkGoCheck =
          name: nativeBuildInputs: command:
          pkgs.runCommand "ladder-${name}"
            {
              nativeBuildInputs =
                with pkgs;
                [
                  go
                  minify
                  tailwindcss
                ]
                ++ nativeBuildInputs;
            }
            ''
              ${prepareCheckSource}
              ${command}
              touch "$out"
            '';

        test-check = mkGoCheck "test" [ ] "go test ./...";
        vet-check = mkGoCheck "vet" [ ] "go vet ./...";
        lint-check = mkGoCheck "lint" [ pkgs.golangci-lint ] ''
          golangci-lint run -c .golangci-lint.yaml
        '';
        format-check =
          pkgs.runCommand "ladder-format"
            {
              nativeBuildInputs = with pkgs; [
                gofumpt
                nixfmt
              ];
            }
            ''
              cp -R ${src}/. source
              chmod -R u+w source
              cd source
              unformatted="$(gofumpt -l .)"
              if [[ -n "$unformatted" ]]; then
                echo "Go files need formatting:" >&2
                echo "$unformatted" >&2
                exit 1
              fi
              nixfmt --check flake.nix
              touch "$out"
            '';

        mkApp =
          drv: description:
          flake-utils.lib.mkApp { inherit drv; }
          // {
            meta = { inherit description; };
          };
        mkLadderApp = description: {
          type = "app";
          program = "${ladder}/bin/ladder";
          meta = { inherit description; };
        };
      in
      {
        packages = {
          default = ladder;
          inherit ladder linux-amd64;
        };

        apps = {
          default = mkLadderApp "Run Ladder";
          run = mkLadderApp "Run Ladder";
          assets = mkApp assets "Generate the embedded stylesheet";
          dev = mkApp dev "Start the live-reloading development server";
          test = mkApp test "Run deterministic tests";
          integration = mkApp integration "Run live external integration tests";
          vet = mkApp vet "Run go vet";
          fmt-check = mkApp fmt-check "Check Go and Nix formatting";
          fmt = mkApp fmt "Format Go and Nix sources";
          lint = mkApp lint "Run formatting and lint checks";
          lint-fix = mkApp lint-fix "Apply formatting and automatic lint fixes";
          tidy = mkApp tidy "Update Go module metadata";
        };

        checks = {
          package = ladder;
          test = test-check;
          vet = vet-check;
          lint = lint-check;
          format = format-check;
        };

        formatter = pkgs.nixfmt;

        devShells.default = pkgs.mkShellNoCC {
          packages = [
            assets
            dev
            fmt
            fmt-check
            integration
            lint
            lint-fix
            test
            tidy
            vet
            pkgs.air
            pkgs.go
            pkgs.gofumpt
            pkgs.golangci-lint
            pkgs.goreleaser
            pkgs.nixfmt
          ];

          RULESET = "./ruleset.yaml";
          LANG = "en_US.UTF-8";
          LC_ALL = "en_US.UTF-8";

          shellHook = ''
            if [[ ! -f cmd/styles.css ]]; then
              ladder-assets
            fi

            echo "ladder devshell: $(go version)"
            echo "commands:"
            echo "  ladder-dev          # live reload at http://localhost:8090"
            echo "  ladder-test         # deterministic test suite"
            echo "  ladder-integration  # live external test suite"
            echo "  ladder-lint         # formatting and lint checks"
            echo "  ladder-fmt          # format Go and Nix sources"
          '';
        };
      }
    );
}
