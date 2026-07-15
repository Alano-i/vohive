/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#e9eefa',
          100: '#d3dcf5',
          200: '#becdf0',
          300: '#92abe4',
          400: '#6689da',
          500: '#4776df',
          600: '#2557ca',
          700: '#1947ad',
          800: '#173b8d',
          900: '#173570',
          950: '#10244e'
        }
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif']
      }
    }
  },
  plugins: []
}
