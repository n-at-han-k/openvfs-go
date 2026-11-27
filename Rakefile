require "rake"
require "pathname"
require "fileutils"

SOURCE_DIRS = ["src", "packages"].freeze
BUILD_DIR = "build"

# The gob on PATH, unless GOB_CHECKOUT points at a gob working copy, which lets
# an unreleased fix be tried against this tree before it ships as a gem.
GOB = if (checkout = ENV["GOB_CHECKOUT"])
  "ruby -I#{checkout}/lib #{checkout}/exe/gob"
else
  "gob"
end

desc "Default: build"
task default: [:build]

desc "Transpile every .gob source into #{BUILD_DIR}/ and compile it"
task :build do
  FileUtils.rm_rf(BUILD_DIR)
  FileUtils.mkdir_p(BUILD_DIR)
  sh "#{GOB} transpile src -o ./#{BUILD_DIR}/src"
  sh "#{GOB} transpile packages -o ./#{BUILD_DIR}/packages"
  FileUtils.cp("go.mod", "#{BUILD_DIR}/go.mod")
  FileUtils.cp("go.sum", "#{BUILD_DIR}/go.sum")
  cd BUILD_DIR do
    sh "go build -o openvfs ./src"
  end
end

desc "Build, then mount ROOT at MOUNTPOINT (defaults under tmp/)"
task run: [:build] do
  root = ENV.fetch("ROOT", "tmp/root")
  mountpoint = ENV.fetch("MOUNTPOINT", "tmp/mnt")
  FileUtils.mkdir_p(root)
  FileUtils.mkdir_p(mountpoint)
  sh "./#{BUILD_DIR}/openvfs #{Pathname.new(root).expand_path} #{Pathname.new(mountpoint).expand_path}"
end

desc "Transpile every .gob source into #{BUILD_DIR}/"
task :transpile do
  sh "#{GOB} transpile #{SOURCE_DIRS.join(" ")} -o ./#{BUILD_DIR}"
end
