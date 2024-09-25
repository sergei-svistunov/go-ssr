const path = require('path');
const fs = require('fs');
const fsPromises = require('fs').promises;

class AssetMapPlugin {
    constructor(options = {}) {
        this.options = options;
        this.cachedEntries = {}; // Cache to track current entries
    }

    apply(compiler) {
        const walkDirSync = (dir) => {
            const entryName = dir === '.' ? 'main' : path.join('pages', dir);
            const dirPath = path.join(__dirname, 'pages', dir);
            const imports = [];

            try {
                const files = fs.readdirSync(dirPath); // Sync read

                files.forEach((file) => {
                    const filePath = path.join(__dirname, 'pages', dir, file);
                    const stat = fs.statSync(filePath); // Sync stat

                    if (stat.isDirectory()) {
                        walkDirSync(path.join(dir, file)); // Recursively walk directories
                    } else if (this.isImage(file) || file === 'index.ts' || file === 'styles.scss') {
                        imports.push(filePath);
                    }
                });

                this.cachedEntries[entryName] = {import: imports}; // Cache the new entry

            } catch (err) {
                console.error(`Error processing directory ${dirPath}:`, err);
                throw err; // Exit on error to prevent incorrect entry generation
            }
        };

        compiler.options.watchOptions = {
            ignored: ['**/*.go', '**/pages/webpack-assets.json']
        };

        compiler.hooks.entryOption.tap('AssetMapPlugin', (context, entry) => {
            walkDirSync('.'); // Start from the root directory
            Object.assign(entry, this.cachedEntries); // Set the initial entries
        });

        compiler.hooks.watchRun.tapAsync('AssetMapPlugin', (watching, callback) => {
            console.log('Detected changes. Rebuilding entries...');
            walkDirSync('.'); // Re-scan directories
            Object.assign(compiler.options.entry, this.cachedEntries); // Update the entries on re-run
            callback(); // Signal async completion
        });

        compiler.hooks.afterCompile.tapAsync('AssetMapPlugin', (compilation, callback) => {
            const watchDir = path.join(__dirname, 'pages');
            compilation.contextDependencies.add(watchDir); // Add the 'pages' directory to the context dependencies
            callback(); // Signal async completion
        });

        compiler.hooks.afterEmit.tapAsync('AssetMapPlugin', async (compilation, callback) => {
            const outputPath = path.join(__dirname, 'webpack-assets.json');

            const entrypoints = {};
            const images = {};
            const publicPath = compilation.options.output.publicPath || '/';

            try {
                // Generate the entry point mapping
                for (const [entryName, entry] of compilation.entrypoints) {
                    entrypoints[entryName] = entry.getFiles().map(f => path.join(publicPath, f));
                }

                // Generate the image path mapping
                for (const [entryName, info] of compilation.assetsInfo) {
                    if (this.isImage(entryName)) {
                        images[info.sourceFilename.replace(/^pages\//, '')] = path.join(publicPath, entryName);
                    }
                }

                // Write the output JSON file after emit phase
                await fsPromises.writeFile(
                    outputPath,
                    JSON.stringify({
                        entrypoints,
                        images,
                    }, null, 2)
                );

                callback(); // Signal async completion

            } catch (err) {
                console.error('Error during afterEmit phase:', err);
                callback(err); // Stop compilation on error
            }
        });
    }

    isImage(assetName) {
        return /\.(png|jpe?g|gif|svg)$/i.test(assetName);
    }
}

module.exports = AssetMapPlugin;
