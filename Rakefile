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

desc "build"
task :build do
  sh "#{GOB} run src/main.gob"
end

desc "Transpile every .gob source into #{BUILD_DIR}/"
task :transpile do
  sh "#{GOB} transpile #{SOURCE_DIRS.join(" ")} -o ./#{BUILD_DIR}"
end
