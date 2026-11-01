const path = require('path');
const MiniCssExtractPlugin = require('mini-css-extract-plugin');
const GoSSRAssetsPlugin = require('gossr-assets-webpack-plugin');

module.exports = {
    entry: {},
    // Development builds default to eval source maps, which the
    // Content-Security-Policy blocks.
    devtool: 'source-map',
    output: {
        filename: 'js/[name].[chunkhash].js',
        path: path.resolve(__dirname, 'static'),
        publicPath: '/static/',
        clean: true
    },
    stats: {warnings: false},
    cache: {
        type: 'filesystem'
    },
    module: {
        rules: [
            {
                test: /\.ts$/,
                use: 'ts-loader',
                exclude: /node_modules/,
            },
            {
                test: /\.css$/,
                use: [
                    MiniCssExtractPlugin.loader,
                    'css-loader',
                ],
            },
            {
                test: /\.scss$/,
                use: [
                    MiniCssExtractPlugin.loader,
                    'css-loader',
                    'sass-loader',
                ],
            },
            {
                test: /\.(png|jpe?g|gif|svg)$/i,
                type: 'asset/resource',
                generator: {
                    filename: (pathData) => {
                        const newPath = pathData.module.resourceResolveData.relativePath
                            .replace(/^\.?\/pages\//, '');
                        return `images/${newPath}[name].[hash][ext]`;
                    }
                },
            },
        ],
    },
    resolve: {
        extensions: ['.ts', '.js'],
    },
    plugins: [
        new GoSSRAssetsPlugin(),
        new MiniCssExtractPlugin({
            filename: 'css/[name].[chunkhash].css',
        }),
    ],
    optimization: {
        // One webpack runtime per page, so modules shared by the page's entries
        // run once: the live connection is one per page.
        runtimeChunk: 'single',
        splitChunks: {
            chunks: 'all',
            hidePathInfo: true,
            cacheGroups: {
                defaultVendors: {
                    test: /[\\/]node_modules[\\/]/,
                    priority: -10,
                    reuseExistingChunk: true,
                },
                default: {
                    minChunks: 2,
                    priority: -20,
                    reuseExistingChunk: true,
                },
            },
        },
    }
}