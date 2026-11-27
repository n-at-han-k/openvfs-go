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

desc "Report the gob version in use"
task :version do
  sh "#{GOB} version"
end

desc "Format every .gob source in place"
task :fmt do
  sh "#{GOB} fmt #{SOURCE_DIRS.join(" ")}"
end

desc "Validate every .gob source"
task :vet do
  sh "#{GOB} vet #{SOURCE_DIRS.join(" ")}"
end

desc "Transpile every .gob source into #{BUILD_DIR}/"
task :transpile do
  sh "#{GOB} transpile #{SOURCE_DIRS.join(" ")} -o ./#{BUILD_DIR}"
end

desc "Check the generated Go is gofmt clean"
task gofmt: [:transpile] do
  unclean = `gofmt -l #{BUILD_DIR}`.split("\n")

  abort "gofmt unclean:\n  #{unclean.join("\n  ")}" if unclean.any?

  puts "gofmt clean: #{Dir["#{BUILD_DIR}/**/*.go"].size} files"
end

desc "Diff the generated Go against the checked-in .go reference"
task diff: [:transpile] do
  SOURCE_DIRS.each do |dir|
    Dir["#{dir}/**/*.gob"].sort.each do |source|
      reference = "#{source.delete_suffix(".gob")}.go"
      generated = File.join(BUILD_DIR, Pathname(reference).relative_path_from(dir).to_s)
      next unless File.exist?(reference) && File.exist?(generated)

      changed = `diff #{generated} #{reference}`.lines.count { |line| line.start_with?("<", ">") }
      puts format("%5d  %s", changed, source) if changed.positive?
    end
  end
end

desc "Remove generated Go"
task :clean do
  FileUtils.rm_rf(BUILD_DIR)
end

desc "vet and transpile every .gob source, and gofmt-check the result"
task check: [:vet, :gofmt]
